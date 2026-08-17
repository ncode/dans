package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// ListenAndServe binds the configured address before entering Serve, avoiding
// a cancellation race between listener creation and graceful shutdown.
func ListenAndServe(ctx context.Context, server *http.Server, health *Health, shutdownTimeout time.Duration) error {
	if server == nil {
		return errors.New("serve: missing HTTP server")
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	return Serve(ctx, server, health, listener, shutdownTimeout)
}

// Serve handles requests on an already-bound listener and drains after
// cancellation. Cancellation remains in the returned error for CLI exit 130.
func Serve(ctx context.Context, server *http.Server, health *Health, listener net.Listener, shutdownTimeout time.Duration) error {
	if ctx == nil || server == nil || health == nil || listener == nil || shutdownTimeout < time.Millisecond {
		return errors.New("serve: invalid configuration")
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownErr := GracefulShutdown(context.WithoutCancel(ctx), server, health, shutdownTimeout)
		serveErr := <-served
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(ctx.Err(), shutdownErr, serveErr)
	}
}
