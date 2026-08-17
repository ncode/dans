package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ncode/dans/internal/contract"
	"github.com/ncode/dans/internal/upstream"
)

func TestCompatibilityBlocksAuthenticatedTrafficWhenSchemaIsUnavailable(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := Compatibility(CompatibilityConfig{
		Schema:             func(context.Context) error { return errors.New("private database detail") },
		PowerDNSCompatible: func() bool { return true },
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestWithRoute(contract.AccessAuthenticatedRead, "/api/v1/dans/me"))

	if recorder.Code != http.StatusServiceUnavailable || calls.Load() != 0 {
		t.Fatalf("status = %d, downstream calls = %d", recorder.Code, calls.Load())
	}
	if body := recorder.Body.String(); body == "" || strings.Contains(body, "private database detail") {
		t.Errorf("response exposed dependency detail: %q", body)
	}
}

func TestCompatibilityBlocksOnlyPowerDNSRoutesWhenUpstreamIsIncompatible(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := Compatibility(CompatibilityConfig{
		Schema:             func(context.Context) error { return nil },
		PowerDNSCompatible: func() bool { return false },
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, requestWithRoute(contract.AccessAuthenticatedRead, "/api/v1/servers/{server_id}/zones"))
	if blocked.Code != http.StatusServiceUnavailable || calls.Load() != 0 {
		t.Fatalf("PowerDNS status = %d, downstream calls = %d", blocked.Code, calls.Load())
	}

	dans := httptest.NewRecorder()
	handler.ServeHTTP(dans, requestWithRoute(contract.AccessAuthenticatedRead, "/api/v1/dans/me"))
	if dans.Code != http.StatusNoContent || calls.Load() != 1 {
		t.Fatalf("DANS status = %d, downstream calls = %d", dans.Code, calls.Load())
	}
}

func TestCompatibilityLeavesAnonymousHealthProbesIndependent(t *testing.T) {
	t.Parallel()

	handler := Compatibility(CompatibilityConfig{
		Schema:             func(context.Context) error { t.Fatal("schema probed"); return nil },
		PowerDNSCompatible: func() bool { t.Fatal("PowerDNS state read"); return false },
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestWithRoute(contract.AccessAnonymous, "/readyz"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestPowerDNSCompatibilityPreservesKnownVersionAcrossTransientProbeFailure(t *testing.T) {
	t.Parallel()

	state := &PowerDNSCompatibility{}
	if state.Compatible() {
		t.Fatal("new state is compatible before a successful probe")
	}
	state.Record(nil)
	if !state.Compatible() {
		t.Fatal("successful probe did not mark compatible")
	}
	state.Record(upstream.ErrUpstreamProbe)
	if !state.Compatible() {
		t.Fatal("transient probe failure discarded the last known compatible version")
	}
	var calls atomic.Int64
	handler := Compatibility(CompatibilityConfig{
		Schema: func(context.Context) error { return nil }, PowerDNSCompatible: state.Compatible,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	afterTransient := httptest.NewRecorder()
	handler.ServeHTTP(afterTransient, requestWithRoute(contract.AccessAuthenticatedRead, "/api/v1/servers"))
	if afterTransient.Code != http.StatusNoContent || calls.Load() != 1 {
		t.Fatalf("after transient probe = status %d, downstream calls %d", afterTransient.Code, calls.Load())
	}
	state.Record(upstream.ErrIncompatibleVersion)
	if state.Compatible() {
		t.Fatal("incompatible version did not mark incompatible")
	}
	afterIncompatible := httptest.NewRecorder()
	handler.ServeHTTP(afterIncompatible, requestWithRoute(contract.AccessAuthenticatedRead, "/api/v1/servers"))
	if afterIncompatible.Code != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatalf("after incompatible probe = status %d, downstream calls %d", afterIncompatible.Code, calls.Load())
	}
}

func requestWithRoute(class contract.AccessClass, template string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, template, nil)
	ctx := context.WithValue(request.Context(), routeInfoKey, RouteInfo{Template: template, OperationID: "operation", AccessClass: class})
	ctx = context.WithValue(ctx, requestIDKey, "request-123")
	return request.WithContext(ctx)
}
