package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	contractdoc "github.com/ncode/dans/api/openapi"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

func TestApplicationHandlerAssemblesTrustBoundaryInOrder(t *testing.T) {
	t.Parallel()

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/servers/localhost/zones" {
			t.Errorf("upstream path = %q", request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"example.","extension":"preserved"}]`)
	}))
	t.Cleanup(upstreamServer.Close)
	client, err := upstream.New(
		upstream.TransportConfig{URL: upstreamServer.URL, Timeout: time.Second},
		httpapi.NewSecret("powerdns-key"),
	)
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}

	var authCalls atomic.Int64
	authenticator := authenticatorFunc(func(context.Context, string) (database.Actor, error) {
		authCalls.Add(1)
		return database.Actor{
			IdentityID: "00000000-0000-4000-8000-000000000010",
			TokenID:    "00000000-0000-4000-8000-000000000020",
			Operator:   true,
		}, nil
	})
	health, err := NewHealth(passingDependencies(), time.Second)
	if err != nil {
		t.Fatalf("construct health: %v", err)
	}
	var logs bytes.Buffer
	handler, err := NewApplicationHandler(ApplicationConfig{
		Boundary: BoundaryConfig{
			MaxBodyBytes:   1 << 20,
			RequestTimeout: time.Second,
			MaxConcurrent:  8,
			NewRequestID: func() (string, error) {
				return "00000000-0000-4000-8000-000000000099", nil
			},
		},
		Logger:            NewJSONLogger(&logs, slog.LevelInfo),
		Authenticator:     authenticator,
		Schema:            func(context.Context) error { return nil },
		PowerDNSReady:     func() bool { return true },
		Health:            health,
		Upstream:          client,
		DANS:              &testDANSHandler{},
		Mutations:         &recordingMutationStore{},
		Lifecycle:         &recordingZoneLifecycle{},
		Denials:           &recordingDenialAuditor{},
		AuditFailures:     &recordingAuditFailureReporter{},
		LifecycleFailures: &recordingAuditFailureReporter{},
		UpstreamID:        "primary",
		MutationTimeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("NewApplicationHandler: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/localhost/zones?dnssec=false", nil)
	request.Header.Set("X-API-Key", validToken())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `[{"id":"example.","extension":"preserved"}]` {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if authCalls.Load() != 1 {
		t.Errorf("authentication calls = %d", authCalls.Load())
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("decode access log: %v\n%s", err, logs.String())
	}
	for key, want := range map[string]string{
		"route_template":   "/api/v1/servers/{server_id}/zones",
		"api_operation_id": "listZones",
		"actor_id":         "00000000-0000-4000-8000-000000000010",
		"upstream_outcome": "response",
	} {
		if got := entry[key]; got != want {
			t.Errorf("log %s = %#v, want %q", key, got, want)
		}
	}
}

func TestApplicationHandlerKeepsHealthAnonymousAndDocsAuthenticated(t *testing.T) {
	t.Parallel()

	upstreamServer := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upstreamServer.Close)
	client, err := upstream.New(
		upstream.TransportConfig{URL: upstreamServer.URL, Timeout: time.Second},
		httpapi.NewSecret("powerdns-key"),
	)
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	health, err := NewHealth(passingDependencies(), time.Second)
	if err != nil {
		t.Fatalf("construct health: %v", err)
	}
	var authCalls atomic.Int64
	handler, err := NewApplicationHandler(ApplicationConfig{
		Boundary: BoundaryConfig{MaxBodyBytes: 1 << 20, RequestTimeout: time.Second, MaxConcurrent: 8},
		Logger:   NewJSONLogger(io.Discard, slog.LevelInfo),
		Authenticator: authenticatorFunc(func(context.Context, string) (database.Actor, error) {
			authCalls.Add(1)
			return database.Actor{IdentityID: "00000000-0000-4000-8000-000000000010", TokenID: "00000000-0000-4000-8000-000000000020"}, nil
		}),
		Schema:            func(context.Context) error { return nil },
		PowerDNSReady:     func() bool { return true },
		Health:            health,
		Upstream:          client,
		DANS:              &testDANSHandler{},
		Mutations:         &recordingMutationStore{},
		Lifecycle:         &recordingZoneLifecycle{},
		Denials:           &recordingDenialAuditor{},
		AuditFailures:     &recordingAuditFailureReporter{},
		LifecycleFailures: &recordingAuditFailureReporter{},
		UpstreamID:        "primary",
		MutationTimeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("NewApplicationHandler: %v", err)
	}

	for _, path := range []string{"/livez", "/readyz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.Len() != 0 {
			t.Errorf("%s response = %d %q", path, response.Code, response.Body.String())
		}
	}
	if authCalls.Load() != 0 {
		t.Errorf("health authentication calls = %d", authCalls.Load())
	}

	unauthenticatedDocs := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedDocs, httptest.NewRequest(http.MethodGet, "/api/docs", nil))
	if unauthenticatedDocs.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated docs status = %d", unauthenticatedDocs.Code)
	}
	authenticatedDocsRequest := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	authenticatedDocsRequest.Header.Set("X-API-Key", validToken())
	authenticatedDocs := httptest.NewRecorder()
	handler.ServeHTTP(authenticatedDocs, authenticatedDocsRequest)
	if authenticatedDocs.Code != http.StatusOK || !bytes.Equal(authenticatedDocs.Body.Bytes(), contractdoc.JSON()) {
		t.Errorf("authenticated docs response = %d, exact contract = %t", authenticatedDocs.Code, bytes.Equal(authenticatedDocs.Body.Bytes(), contractdoc.JSON()))
	}

	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/not-declared?secret=never-log", nil))
	if unknown.Code != http.StatusNotFound {
		t.Errorf("unknown route status = %d", unknown.Code)
	}
	if authCalls.Load() != 1 {
		t.Errorf("authentication calls after docs and unknown route = %d, want 1", authCalls.Load())
	}
	if strings.Contains(unknown.Body.String(), "secret") {
		t.Errorf("unknown response leaks query: %s", unknown.Body.String())
	}
}

type testDANSHandler struct{ api.ServerInterface }
