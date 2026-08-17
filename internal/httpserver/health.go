// Package httpserver contains the DANS HTTP trust-boundary and lifecycle.
package httpserver

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"
)

// Probe checks one readiness dependency without exposing its error to callers.
type Probe func(context.Context) error

// AuditHealth latches a failed terminal audit write and clears it only after a
// real persistence probe succeeds.
type AuditHealth struct {
	recovery        Probe
	failed          atomic.Bool
	lifecycleFailed atomic.Bool
}

func NewAuditHealth(recovery Probe) (*AuditHealth, error) {
	if recovery == nil {
		return nil, errors.New("audit health: recovery probe is required")
	}
	return &AuditHealth{recovery: recovery}, nil
}

func (health *AuditHealth) ReportAuditFailure() { health.failed.Store(true) }

// ReportLifecycleFailure latches an upstream-success/local-binding failure.
// Unlike a transient audit failure, it needs explicit reconciliation and must
// not be cleared by a successful audit persistence probe.
func (health *AuditHealth) ReportLifecycleFailure() { health.lifecycleFailed.Store(true) }

func (health *AuditHealth) Failed() bool {
	return health.failed.Load() || health.lifecycleFailed.Load()
}

func (health *AuditHealth) Probe(ctx context.Context) error {
	if err := health.recovery(ctx); err != nil {
		health.failed.Store(true)
		return err
	}
	health.failed.Store(false)
	if health.lifecycleFailed.Load() {
		return errors.New("unreconciled zone lifecycle failure")
	}
	return nil
}

// Dependencies are the four independent readiness requirements.
type Dependencies struct {
	Database Probe
	Schema   Probe
	Audit    Probe
	PowerDNS Probe
}

// Health separates process liveness from current dependency readiness.
type Health struct {
	checks   [4]Probe
	timeout  time.Duration
	draining atomic.Bool
}

// NewHealth builds an immutable, bounded readiness checker.
func NewHealth(dependencies Dependencies, timeout time.Duration) (*Health, error) {
	checks := [4]Probe{dependencies.Database, dependencies.Schema, dependencies.Audit, dependencies.PowerDNS}
	if timeout < time.Millisecond {
		return nil, errors.New("health: timeout must be at least one millisecond")
	}
	for _, check := range checks {
		if check == nil {
			return nil, errors.New("health: every readiness dependency is required")
		}
	}
	return &Health{checks: checks, timeout: timeout}, nil
}

// BeginDrain irreversibly makes the instance unready before listener shutdown.
func (health *Health) BeginDrain() { health.draining.Store(true) }

// Draining reports whether graceful shutdown has begun.
func (health *Health) Draining() bool { return health.draining.Load() }

// Livez reports process HTTP liveness without dependency detail.
func (*Health) Livez(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// Readyz checks current dependencies without caching and returns no detail.
func (health *Health) Readyz(w http.ResponseWriter, request *http.Request) {
	if health.Ready(request.Context()) {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
}

// Ready evaluates current readiness under one shared deadline.
func (health *Health) Ready(parent context.Context) bool {
	if health.draining.Load() {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, health.timeout)
	defer cancel()
	for _, check := range health.checks {
		if err := check(ctx); err != nil {
			return false
		}
	}
	return !health.draining.Load()
}

type shutdownServer interface {
	Shutdown(context.Context) error
	Close() error
}

// GracefulShutdown marks the instance unready, drains within a fixed deadline,
// and force-closes any connection left after that deadline.
func GracefulShutdown(parent context.Context, server shutdownServer, health *Health, timeout time.Duration) error {
	if server == nil || health == nil || timeout < time.Millisecond {
		return errors.New("shutdown: invalid configuration")
	}
	health.BeginDrain()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := server.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.Join(err, server.Close())
	}
	return err
}
