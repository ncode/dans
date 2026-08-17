package httpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeRunsOnProvidedListenerAndDrainsOnCancellation(t *testing.T) {
	t.Parallel()

	health, err := NewHealth(passingDependencies(), time.Second)
	if err != nil {
		t.Fatalf("construct health: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server, err := NewHTTPServer(ServerConfig{
		Address:           listener.Addr().String(),
		MaxHeaderBytes:    16 << 10,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
	}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	if err != nil {
		t.Fatalf("construct HTTP server: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, server, health, listener, time.Second) }()

	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		cancel()
		t.Fatalf("GET served listener: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d", response.StatusCode)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Serve error = %v, want context.Canceled", err)
	}
	if !health.Draining() {
		t.Error("health remained ready after serve cancellation")
	}
}

func TestServeReturnsListenerFailure(t *testing.T) {
	t.Parallel()

	health, err := NewHealth(passingDependencies(), time.Second)
	if err != nil {
		t.Fatalf("construct health: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	server := &http.Server{Handler: http.NotFoundHandler()}
	if err := Serve(t.Context(), server, health, listener, time.Second); err == nil {
		t.Fatal("Serve error = nil for closed listener")
	}
}
