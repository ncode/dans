package cli

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ncode/dans/internal/httpapi"
)

func TestRuntimeConfigurationsMapEveryBound(t *testing.T) {
	t.Parallel()

	config := validRuntimeServeConfig()
	mapped := runtimeConfigurations(config)
	if mapped.database.ConnectionString != config.DatabaseURL.Value() ||
		mapped.database.MaxConnections != config.DatabaseMaxConnections ||
		mapped.database.AuthenticationReserve != config.DatabaseAuthenticationReserve ||
		mapped.database.ReadinessReserve != config.DatabaseReadinessReserve ||
		mapped.database.ConnectTimeout != config.DatabaseConnectTimeout ||
		mapped.database.StatementTimeout != config.DatabaseStatementTimeout ||
		mapped.database.LockTimeout != config.DatabaseLockTimeout ||
		mapped.database.TransactionTimeout != config.DatabaseTransactionTimeout {
		t.Fatalf("database runtime mapping = %#v", mapped.database)
	}
	if mapped.boundary.MaxBodyBytes != config.MaxBodyBytes ||
		mapped.boundary.RequestTimeout != config.RequestTimeout ||
		mapped.boundary.MaxConcurrent != config.MaxConcurrentRequests {
		t.Fatalf("boundary runtime mapping = %#v", mapped.boundary)
	}
	if mapped.server.Address != config.Address ||
		mapped.server.MaxHeaderBytes != config.MaxHeaderBytes ||
		mapped.server.ReadHeaderTimeout != config.ReadHeaderTimeout ||
		mapped.server.ReadTimeout != config.ReadTimeout ||
		mapped.server.WriteTimeout != config.WriteTimeout ||
		mapped.server.IdleTimeout != config.IdleTimeout {
		t.Fatalf("HTTP server runtime mapping = %#v", mapped.server)
	}
	if mapped.upstream.URL != config.PowerDNSURL ||
		mapped.upstream.UnixSocket != config.PowerDNSUnixSocket ||
		mapped.upstream.Timeout != config.PowerDNSTimeout {
		t.Fatalf("PowerDNS runtime mapping = %#v", mapped.upstream)
	}
}

func TestRuntimeServerCancellationUsesBoundedProductionAssembly(t *testing.T) {
	server := NewRuntimeServer(io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := server.Serve(ctx, validRuntimeServeConfig())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Serve error = %v, want context cancellation", err)
	}
}

func TestOverdueIntentCloserSweepsImmediatelyAndPeriodically(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		closer := &recordingOverdueIntentCloser{}
		failures := &recordingRuntimeAuditFailures{}
		go runOverdueIntentCloser(ctx, closer, failures, time.Second)
		synctest.Wait()
		if calls := closer.calls.Load(); calls != 1 {
			t.Fatalf("immediate close calls = %d, want 1", calls)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if calls := closer.calls.Load(); calls != 2 {
			t.Fatalf("periodic close calls = %d, want 2", calls)
		}
		cancel()
		synctest.Wait()
		if failures.calls.Load() != 0 {
			t.Fatalf("failure reports = %d", failures.calls.Load())
		}
	})
}

func TestOverdueIntentCloserLatchesAuditFailure(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		closer := &recordingOverdueIntentCloser{err: errors.New("audit unavailable")}
		failures := &recordingRuntimeAuditFailures{}
		go runOverdueIntentCloser(ctx, closer, failures, time.Second)
		synctest.Wait()
		cancel()
		synctest.Wait()
		if failures.calls.Load() != 1 {
			t.Fatalf("failure reports = %d, want 1", failures.calls.Load())
		}
	})
}

type recordingOverdueIntentCloser struct {
	calls atomic.Int64
	err   error
}

func (closer *recordingOverdueIntentCloser) CloseOverdueDNSIntents(context.Context, int) (int, error) {
	closer.calls.Add(1)
	return 0, closer.err
}

type recordingRuntimeAuditFailures struct{ calls atomic.Int64 }

func (failures *recordingRuntimeAuditFailures) ReportAuditFailure() { failures.calls.Add(1) }

func validRuntimeServeConfig() ServeConfig {
	return ServeConfig{
		Address:                       "127.0.0.1:0",
		DatabaseURL:                   httpapi.NewSecret("postgres://dans@127.0.0.1:1/dans"),
		DatabaseMaxConnections:        12,
		DatabaseAuthenticationReserve: 2,
		DatabaseReadinessReserve:      1,
		DatabaseConnectTimeout:        time.Second,
		DatabaseStatementTimeout:      2 * time.Second,
		DatabaseLockTimeout:           time.Second,
		DatabaseTransactionTimeout:    3 * time.Second,
		PowerDNSURL:                   "http://127.0.0.1:1",
		PowerDNSUpstream:              "default",
		PowerDNSAPIKey:                httpapi.NewSecret("powerdns-key"),
		PowerDNSTimeout:               2 * time.Second,
		MaxBodyBytes:                  1 << 20,
		RequestTimeout:                5 * time.Second,
		MaxConcurrentRequests:         8,
		MaxHeaderBytes:                1 << 20,
		ReadHeaderTimeout:             time.Second,
		ReadTimeout:                   6 * time.Second,
		WriteTimeout:                  6 * time.Second,
		IdleTimeout:                   30 * time.Second,
		HealthTimeout:                 time.Second,
		ShutdownTimeout:               time.Second,
	}
}
