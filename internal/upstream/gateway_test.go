package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/httpapi"
)

func TestForwardUsesTypedValuesOnceAndIsolatesRequestHeaders(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	wantBody := api.ZonePatch{Rrsets: []api.RRSetChange{{
		Changetype: api.RRSetChangeChangetype("REPLACE"),
		Name:       "www.example.org.",
		Records:    &[]api.Record{{Content: "192.0.2.10"}},
		Ttl:        new(300),
		Type:       "A",
	}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodPatch || request.URL.EscapedPath() != "/api/v1/servers/localhost/zones/example.org." {
			t.Errorf("request = %s %s", request.Method, request.URL.EscapedPath())
		}
		if request.Host == "spoofed.invalid" || request.Host == "" {
			t.Errorf("Host was not replaced: %q", request.Host)
		}
		if values := request.Header.Values("X-API-Key"); len(values) != 1 || values[0] != "power-key" {
			t.Errorf("X-API-Key = %q", values)
		}
		if got := request.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		for _, name := range []string{"Connection", "Forwarded", "X-Forwarded-For", "X-Smuggle", "Proxy-Authorization", "X-Client-Only"} {
			if got := request.Header.Values(name); len(got) != 0 {
				t.Errorf("%s leaked upstream: %q", name, got)
			}
		}
		var gotBody api.ZonePatch
		if err := json.NewDecoder(request.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if !reflect.DeepEqual(gotBody, wantBody) {
			t.Errorf("body = %#v, want %#v", gotBody, wantBody)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	response, err := client.Forward(func(generated api.ClientInterface) (*http.Response, error) {
		return generated.PatchZone(t.Context(), "localhost", "example.org.", wantBody, func(_ context.Context, request *http.Request) error {
			request.Host = "spoofed.invalid"
			request.Header.Set("Accept", "application/json")
			request.Header.Set("X-API-Key", "dans-client-token")
			request.Header.Set("Forwarded", "for=192.0.2.1")
			request.Header.Set("X-Forwarded-For", "192.0.2.1")
			request.Header.Set("Connection", "X-Smuggle, keep-alive")
			request.Header.Set("X-Smuggle", "secret")
			request.Header.Set("Proxy-Authorization", "secret")
			request.Header.Set("X-Client-Only", "secret")
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d", response.StatusCode)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestForwardDoesNotRetryMutationAndMapsTransportErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		transport  error
		wantStatus int
	}{
		{name: "connection", transport: errors.New("dial tcp 192.0.2.10:8081: refused"), wantStatus: http.StatusBadGateway},
		{name: "timeout", transport: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, "http://127.0.0.1:8081")
			var calls atomic.Int64
			client.generated.Client = doerFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, tt.transport
			})
			_, err := client.Forward(func(generated api.ClientInterface) (*http.Response, error) {
				return generated.PatchZone(t.Context(), "localhost", "example.org.", api.ZonePatch{Rrsets: []api.RRSetChange{}})
			})
			if status := httpapi.StatusCode(err); status != tt.wantStatus {
				t.Errorf("status = %d, want %d (error %v)", status, tt.wantStatus, err)
			}
			if got := calls.Load(); got != 1 {
				t.Errorf("calls = %d, want 1", got)
			}
			text := fmt.Sprint(err)
			if strings.Contains(text, "192.0.2.10") || strings.Contains(text, "power-key") {
				t.Errorf("public error leaked internal value: %q", text)
			}
		})
	}
}

func TestForwardReturnsGenuineUpstreamHTTPErrorUnwrapped(t *testing.T) {
	t.Parallel()

	body := `{"error":"conflict","future":{"opaque":true}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Extension", "preserved")
		w.Header().Set("Connection", "X-Hop")
		w.Header().Set("X-Hop", "removed")
		w.Header().Set("X-API-Key", "must-not-leak")
		w.Header().Set("Cache-Control", "public")
		w.Header().Set("X-Request-ID", "upstream-id")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	response, err := client.Forward(func(generated api.ClientInterface) (*http.Response, error) {
		return generated.ListZones(t.Context(), "localhost", nil)
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	recorder := httptest.NewRecorder()
	if err := Relay(recorder, response, "00000000-0000-4000-8000-000000000001"); err != nil {
		t.Fatalf("Relay: %v", err)
	}
	result := recorder.Result()
	defer result.Body.Close()
	gotBody, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if result.StatusCode != http.StatusUnauthorized || string(gotBody) != body {
		t.Errorf("response = %d %q", result.StatusCode, gotBody)
	}
	if got := result.Header.Get("X-Upstream-Extension"); got != "preserved" {
		t.Errorf("X-Upstream-Extension = %q", got)
	}
	for _, name := range []string{"Connection", "X-Hop", "X-API-Key"} {
		if got := result.Header.Values(name); len(got) != 0 {
			t.Errorf("%s leaked downstream: %q", name, got)
		}
	}
	if got := result.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := result.Header.Get("X-Request-ID"); got != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("X-Request-ID = %q", got)
	}
}

func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(TransportConfig{URL: endpoint, Timeout: time.Second}, httpapi.NewSecret("power-key"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}
