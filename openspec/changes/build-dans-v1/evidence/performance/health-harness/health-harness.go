package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const (
	pollInterval   = time.Millisecond
	requestTimeout = 200 * time.Millisecond
	runTimeout     = 15 * time.Second
)

type probeResult struct {
	name    string
	elapsed time.Duration
	err     error
}

func docker(args ...string) error {
	cmd := exec.Command("rtk", append([]string{"docker"}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rtk docker %v: %w: %s", args, err, output)
	}
	return nil
}

func probe(ctx context.Context, name, url string, start time.Time, results chan<- probeResult) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: requestTimeout}

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			results <- probeResult{name: name, err: err}
			return
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				results <- probeResult{name: name, elapsed: time.Since(start)}
				return
			}
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			results <- probeResult{name: name, err: ctx.Err()}
			return
		case <-timer.C:
		}
	}
}

func run(container, root string) (time.Duration, time.Duration, error) {
	if err := docker("stop", "--timeout", "3", container); err != nil {
		return 0, 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	results := make(chan probeResult, 2)
	start := time.Now()
	go probe(ctx, "live", root+"/livez", start, results)
	go probe(ctx, "ready", root+"/readyz", start, results)
	if err := docker("start", container); err != nil {
		cancel()
		for range 2 {
			<-results
		}
		return 0, 0, err
	}

	var live, ready time.Duration
	for range 2 {
		result := <-results
		if result.err != nil {
			return 0, 0, fmt.Errorf("%s probe: %w", result.name, result.err)
		}
		switch result.name {
		case "live":
			live = result.elapsed
		case "ready":
			ready = result.elapsed
		}
	}
	return live, ready, nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: health-harness CONTAINER HTTP_ROOT")
		os.Exit(2)
	}
	container, root := os.Args[1], os.Args[2]
	for i := 1; i <= 3; i++ {
		if _, _, err := run(container, root); err != nil {
			fmt.Fprintf(os.Stderr, "warmup %d: %v\n", i, err)
			os.Exit(1)
		}
	}

	fmt.Println("run\tlive_ns\tready_ns")
	for i := 1; i <= 15; i++ {
		live, ready, err := run(container, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "run %d: %v\n", i, err)
			os.Exit(1)
		}
		fmt.Printf("%d\t%d\t%d\n", i, live.Nanoseconds(), ready.Nanoseconds())
	}
}
