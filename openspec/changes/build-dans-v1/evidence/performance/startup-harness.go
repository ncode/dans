package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: startup-harness WARMUPS RUNS COMMAND LABEL [ARG ...]")
		os.Exit(2)
	}
	warmups, err := strconv.Atoi(os.Args[1])
	if err != nil || warmups < 0 {
		fmt.Fprintln(os.Stderr, "invalid warmup count")
		os.Exit(2)
	}
	runs, err := strconv.Atoi(os.Args[2])
	if err != nil || runs < 1 {
		fmt.Fprintln(os.Stderr, "invalid run count")
		os.Exit(2)
	}
	command, label := os.Args[3], os.Args[4]
	args := os.Args[5:]
	run := func() (time.Duration, int64, error) {
		cmd := exec.Command(command, args...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		start := time.Now()
		if err := cmd.Run(); err != nil {
			return 0, 0, err
		}
		usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
		if !ok {
			return 0, 0, fmt.Errorf("unexpected resource usage type %T", cmd.ProcessState.SysUsage())
		}
		return time.Since(start), usage.Maxrss, nil
	}
	for range warmups {
		if _, _, err := run(); err != nil {
			fmt.Fprintf(os.Stderr, "warmup: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println("run\tlabel\telapsed_ns\tmaxrss_kib")
	for i := 1; i <= runs; i++ {
		elapsed, maxRSS, err := run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "run %d: %v\n", i, err)
			os.Exit(1)
		}
		fmt.Printf("%d\t%s\t%d\t%d\n", i, label, elapsed.Nanoseconds(), maxRSS)
	}
}
