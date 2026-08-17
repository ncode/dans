package upstream

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/internal/httpapi"
)

func TestProbeAuthenticatesAndAcceptsSupportedVersion(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/servers/localhost" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if values := r.Header.Values("X-API-Key"); len(values) != 1 || values[0] != "power-key" {
			t.Errorf("X-API-Key = %q", values)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"type":"Server","id":"localhost","daemon_type":"authoritative","version":"5.1.3","url":"/api/v1/servers/localhost"}`)
	}))
	t.Cleanup(server.Close)

	client, err := New(TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("power-key"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := client.Probe(t.Context()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

func TestProbeFailsClosedForRejectedMalformedOrIncompatibleUpstream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "authentication rejected", status: http.StatusUnauthorized, body: `{"error":"invalid key"}`, wantErr: ErrUpstreamAuthentication},
		{name: "forbidden key", status: http.StatusForbidden, body: `{"error":"forbidden"}`, wantErr: ErrUpstreamAuthentication},
		{name: "unexpected status", status: http.StatusServiceUnavailable, body: `{"error":"down"}`, wantErr: ErrUpstreamProbe},
		{name: "malformed JSON", status: http.StatusOK, body: `{`, wantErr: ErrUpstreamProbe},
		{name: "missing version", status: http.StatusOK, body: `{"daemon_type":"authoritative"}`, wantErr: ErrIncompatibleVersion},
		{name: "wrong daemon", status: http.StatusOK, body: `{"daemon_type":"recursor","version":"5.1.3"}`, wantErr: ErrIncompatibleVersion},
		{name: "too old", status: http.StatusOK, body: `{"daemon_type":"authoritative","version":"5.1.2"}`, wantErr: ErrIncompatibleVersion},
		{name: "next minor", status: http.StatusOK, body: `{"daemon_type":"authoritative","version":"5.2.0"}`, wantErr: ErrIncompatibleVersion},
		{name: "next major", status: http.StatusOK, body: `{"daemon_type":"authoritative","version":"6.0.0"}`, wantErr: ErrIncompatibleVersion},
		{name: "unparseable", status: http.StatusOK, body: `{"daemon_type":"authoritative","version":"dev"}`, wantErr: ErrIncompatibleVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			t.Cleanup(server.Close)
			client, err := New(TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("power-key"))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := client.Probe(t.Context()); !errors.Is(err, tt.wantErr) {
				t.Errorf("Probe() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestProbeAcceptsOnlyConfiguredVersionWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		{version: "5.1.3", want: true},
		{version: "5.1.4", want: true},
		{version: "5.1.99", want: true},
		{version: "5.1.3-1", want: true},
		{version: "5.1.3+build", want: true},
		{version: "5.1.2", want: false},
		{version: "5.0.99", want: false},
		{version: "5.2.0", want: false},
		{version: "6.1.3", want: false},
		{version: "v5.1.3", want: false},
		{version: "5.1", want: false},
		{version: "5.1.x", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			if got := supportedVersion(tt.version); got != tt.want {
				t.Errorf("supportedVersion(%q) = %t, want %t", tt.version, got, tt.want)
			}
		})
	}
}

func TestProbeBoundsResponseAndDoesNotExposeCredential(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, strings.Repeat("x", MaxProbeBytes+1))
	}))
	t.Cleanup(server.Close)
	const key = "upstream-secret-value"
	client, err := New(TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret(key))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = client.Probe(t.Context())
	if !errors.Is(err, ErrUpstreamProbe) {
		t.Fatalf("Probe() error = %v, want ErrUpstreamProbe", err)
	}
	if strings.Contains(fmt.Sprint(err), key) {
		t.Errorf("Probe() error exposed credential: %v", err)
	}
}

func TestNewRejectsEmptyKey(t *testing.T) {
	t.Parallel()

	if _, err := New(TransportConfig{URL: "http://127.0.0.1:8081", Timeout: time.Second}, httpapi.NewSecret("")); err == nil {
		t.Fatal("New() error = nil for empty key")
	}
}
