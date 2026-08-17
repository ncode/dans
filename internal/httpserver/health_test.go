package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestHealthSeparatesLivenessFromDependencyReadiness(t *testing.T) {
	t.Parallel()

	dependencyErr := errors.New("postgres target and credentials must remain private")
	checks := []struct {
		name string
		set  func(*Dependencies)
	}{
		{name: "database", set: func(dependencies *Dependencies) { dependencies.Database = failingProbe(dependencyErr) }},
		{name: "schema", set: func(dependencies *Dependencies) { dependencies.Schema = failingProbe(dependencyErr) }},
		{name: "audit", set: func(dependencies *Dependencies) { dependencies.Audit = failingProbe(dependencyErr) }},
		{name: "powerdns", set: func(dependencies *Dependencies) { dependencies.PowerDNS = failingProbe(dependencyErr) }},
	}
	for _, tt := range checks {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dependencies := passingDependencies()
			tt.set(&dependencies)
			health, err := NewHealth(dependencies, time.Second)
			if err != nil {
				t.Fatalf("NewHealth: %v", err)
			}
			assertProbe(t, health.Livez, http.StatusOK)
			assertProbe(t, health.Readyz, http.StatusServiceUnavailable)
		})
	}
}

func TestHealthIsReadyOnlyWhenAllChecksPassAndNotDraining(t *testing.T) {
	t.Parallel()

	health, err := NewHealth(passingDependencies(), time.Second)
	if err != nil {
		t.Fatalf("NewHealth: %v", err)
	}
	assertProbe(t, health.Livez, http.StatusOK)
	assertProbe(t, health.Readyz, http.StatusOK)

	health.BeginDrain()
	assertProbe(t, health.Livez, http.StatusOK)
	assertProbe(t, health.Readyz, http.StatusServiceUnavailable)
}

func TestReadinessCheckHasOneBoundedDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		dependencies := passingDependencies()
		dependencies.Database = func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}
		health, err := NewHealth(dependencies, 250*time.Millisecond)
		if err != nil {
			t.Fatalf("NewHealth: %v", err)
		}
		assertProbe(t, health.Readyz, http.StatusServiceUnavailable)
		if elapsed := time.Since(start); elapsed != 250*time.Millisecond {
			t.Errorf("elapsed = %s", elapsed)
		}
	})
}

func TestAuditHealthLatchesOutcomeFailureUntilPersistenceProbeRecovers(t *testing.T) {
	t.Parallel()

	probeErr := errors.New("audit insert rejected")
	audit, err := NewAuditHealth(func(context.Context) error { return probeErr })
	if err != nil {
		t.Fatalf("NewAuditHealth: %v", err)
	}
	audit.ReportAuditFailure()
	if !audit.Failed() {
		t.Fatal("audit failure was not latched")
	}
	if err := audit.Probe(t.Context()); !errors.Is(err, probeErr) || !audit.Failed() {
		t.Fatalf("failed probe = %v, latched=%t", err, audit.Failed())
	}

	audit.recovery = func(context.Context) error { return nil }
	if err := audit.Probe(t.Context()); err != nil || audit.Failed() {
		t.Fatalf("recovered probe = %v, latched=%t", err, audit.Failed())
	}
}

func TestAuditHealthKeepsUnreconciledLifecycleFailureSticky(t *testing.T) {
	t.Parallel()

	audit, err := NewAuditHealth(func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("NewAuditHealth: %v", err)
	}
	audit.ReportLifecycleFailure()
	if err := audit.Probe(t.Context()); err == nil || !audit.Failed() {
		t.Fatalf("lifecycle probe = %v, failed=%t", err, audit.Failed())
	}
}

func TestGracefulShutdownMarksUnreadyBeforeBoundedDrain(t *testing.T) {
	t.Parallel()

	health, err := NewHealth(passingDependencies(), time.Second)
	if err != nil {
		t.Fatalf("NewHealth: %v", err)
	}
	server := &recordingShutdownServer{health: health}
	if err := GracefulShutdown(t.Context(), server, health, time.Second); err != nil {
		t.Fatalf("GracefulShutdown: %v", err)
	}
	if !reflect.DeepEqual(server.events, []string{"shutdown-unready"}) {
		t.Errorf("events = %q", server.events)
	}
}

func TestGracefulShutdownForceClosesAfterDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		health, err := NewHealth(passingDependencies(), time.Second)
		if err != nil {
			t.Fatalf("NewHealth: %v", err)
		}
		server := &recordingShutdownServer{health: health, waitForDeadline: true}
		err = GracefulShutdown(t.Context(), server, health, 100*time.Millisecond)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("GracefulShutdown error = %v", err)
		}
		if !reflect.DeepEqual(server.events, []string{"shutdown-unready", "close"}) {
			t.Errorf("events = %q", server.events)
		}
	})
}

func passingDependencies() Dependencies {
	pass := func(context.Context) error { return nil }
	return Dependencies{Database: pass, Schema: pass, Audit: pass, PowerDNS: pass}
}

func failingProbe(err error) Probe {
	return func(context.Context) error { return err }
}

func assertProbe(t *testing.T, handler http.HandlerFunc, wantStatus int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	result := recorder.Result()
	defer result.Body.Close()
	if result.StatusCode != wantStatus {
		t.Errorf("status = %d, want %d", result.StatusCode, wantStatus)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("probe body exposed details: %q", recorder.Body.String())
	}
}

type recordingShutdownServer struct {
	health          *Health
	waitForDeadline bool
	events          []string
}

func (server *recordingShutdownServer) Shutdown(ctx context.Context) error {
	state := "ready"
	if server.health.Draining() {
		state = "unready"
	}
	server.events = append(server.events, "shutdown-"+state)
	if server.waitForDeadline {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (server *recordingShutdownServer) Close() error {
	server.events = append(server.events, "close")
	return nil
}
