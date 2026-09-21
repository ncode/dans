package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ncode/dans/internal/httpapi"
)

// Middleware is one ordered HTTP trust-boundary stage.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware in the order listed.
func Chain(handler http.Handler, middleware ...Middleware) http.Handler {
	for index := len(middleware) - 1; index >= 0; index-- {
		handler = middleware[index](handler)
	}
	return handler
}

// BoundaryConfig bounds resources before contract validation or authentication.
type BoundaryConfig struct {
	MaxBodyBytes   int64
	RequestTimeout time.Duration
	MaxConcurrent  int
	NewRequestID   func() (string, error)
}

// NewBoundary constructs the outer request-ID, recovery, body, deadline, and
// concurrency stage.
func NewBoundary(config BoundaryConfig) (Middleware, error) {
	if config.MaxBodyBytes <= 0 || config.RequestTimeout < time.Millisecond || config.MaxConcurrent <= 0 {
		return nil, errors.New("HTTP boundary: limits must be positive and bounded")
	}
	if config.NewRequestID == nil {
		config.NewRequestID = httpapi.NewRequestID
	}
	semaphore := make(chan struct{}, config.MaxConcurrent)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			requestID, err := config.NewRequestID()
			setSecurityHeaders(w.Header())
			if err != nil {
				httpapi.WriteError(w, "", httpapi.NewError(httpapi.KindInternal, err))
				return
			}
			w.Header().Set("X-Request-ID", requestID)
			parent := WithRequestID(request.Context(), requestID)
			if isAuditExport(request) {
				parent = context.WithValue(parent, exportParentKey, parent)
			}
			ctx, cancel := context.WithTimeout(parent, config.RequestTimeout)
			defer cancel()
			request = request.WithContext(ctx)
			request.Body = http.MaxBytesReader(w, request.Body, config.MaxBodyBytes)

			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindUnavailable, ctx.Err()))
				return
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					if recovered == http.ErrAbortHandler {
						panic(recovered)
					}
					httpapi.WriteError(w, requestID, httpapi.NewError(httpapi.KindInternal, errors.New("HTTP handler panic")))
				}
			}()
			next.ServeHTTP(w, request)
		})
	}, nil
}

func setSecurityHeaders(header http.Header) {
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
}

// ServerConfig fixes the net/http connection-level resource limits.
type ServerConfig struct {
	Address           string
	MaxHeaderBytes    int
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// NewHTTPServer creates a server without opening a listener.
func NewHTTPServer(config ServerConfig, handler http.Handler) (*http.Server, error) {
	if config.Address == "" || handler == nil || config.MaxHeaderBytes <= 0 || config.ReadHeaderTimeout < time.Millisecond || config.ReadTimeout < time.Millisecond || config.WriteTimeout < time.Millisecond || config.IdleTimeout < time.Millisecond {
		return nil, errors.New("HTTP server: invalid bounded configuration")
	}
	return &http.Server{
		Addr:              config.Address,
		Handler:           handler,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		MaxHeaderBytes:    config.MaxHeaderBytes,
	}, nil
}
