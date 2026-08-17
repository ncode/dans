package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDNSCommandSurfaceCoversCommonWorkflows(t *testing.T) {
	root := NewCommand(Options{Version: "test", Streams: discardStreams()})
	paths := [][]string{
		{"zones", "list"}, {"zones", "get"}, {"zones", "create"}, {"zones", "delete"},
		{"rrsets", "list"}, {"rrsets", "get"}, {"rrsets", "apply"}, {"rrsets", "replace"}, {"rrsets", "add-value"},
		{"rrsets", "remove-value"}, {"rrsets", "delete"},
		{"health", "live"}, {"health", "ready"}, {"docs"},
	}
	for _, path := range paths {
		if commandAt(root, path...) == nil {
			t.Errorf("missing command %q", path)
		}
	}
}

func TestHealthUsesGeneratedClientAtRootWithoutCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/livez" {
			t.Errorf("request path = %q, want /livez", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			t.Errorf("anonymous liveness request has Authorization %q", authorization)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	t.Setenv("DANS_ENDPOINT", server.URL+"/api/v1")
	t.Setenv("DANS_API_TOKEN", "")

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--output", "json", "health", "live"}, Options{
		Version:    "test",
		Streams:    Streams{Out: &stdout, Err: &stderr},
		HTTPClient: server.Client(),
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestDocsUsesDANSAPIKeyAtRoot(t *testing.T) {
	token := newTestToken(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/docs" {
			t.Errorf("request path = %q, want /api/docs", request.URL.Path)
		}
		if got := request.Header.Values("X-API-Key"); len(got) != 1 || got[0] != token {
			t.Errorf("X-API-Key = %q, want exactly one DANS token", got)
		}
		if got := request.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("DANS_ENDPOINT", server.URL+"/api/v1")
	t.Setenv("DANS_API_TOKEN", token)

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--output", "json", "docs"}, Options{
		Version:    "test",
		Streams:    Streams{Out: &stdout, Err: &stderr},
		HTTPClient: server.Client(),
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRRSetIndividualValueOperationsUseOneGeneratedPatch(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		changeType string
		extraArgs  []string
	}{
		{name: "add PTR", command: "add-value", changeType: "EXTEND", extraArgs: []string{"--ttl", "300"}},
		{name: "remove PTR", command: "remove-value", changeType: "PRUNE", extraArgs: []string{"--ttl", "300"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := newTestToken(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != http.MethodPatch || request.URL.Path != "/api/v1/servers/localhost/zones/reverse.test." {
					t.Errorf("request = %s %s", request.Method, request.URL.Path)
				}
				var patch struct {
					RRSets []struct {
						ChangeType string `json:"changetype"`
						Name       string `json:"name"`
						Records    []struct {
							Content string `json:"content"`
						} `json:"records"`
						TTL  *int   `json:"ttl"`
						Type string `json:"type"`
					} `json:"rrsets"`
				}
				if err := json.NewDecoder(request.Body).Decode(&patch); err != nil {
					t.Errorf("decode patch: %v", err)
				}
				if len(patch.RRSets) != 1 {
					t.Errorf("RRset changes = %d, want 1", len(patch.RRSets))
				} else {
					change := patch.RRSets[0]
					if change.ChangeType != tt.changeType || change.Name != "1.2.0.192.in-addr.arpa." || change.Type != "PTR" {
						t.Errorf("change = %#v", change)
					}
					if len(change.Records) != 1 || change.Records[0].Content != "host.example." {
						t.Errorf("records = %#v", change.Records)
					}
					if change.TTL == nil || *change.TTL != 300 {
						t.Errorf("TTL = %#v, want 300", change.TTL)
					}
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)

			t.Setenv("DANS_ENDPOINT", server.URL+"/api/v1")
			t.Setenv("DANS_API_TOKEN", token)
			args := []string{"rrsets", tt.command, "reverse.test.", "1.2.0.192.in-addr.arpa.", "PTR", "host.example."}
			args = append(args, tt.extraArgs...)
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), args, Options{
				Version:    "test",
				Streams:    Streams{Out: &stdout, Err: &stderr},
				HTTPClient: server.Client(),
			})
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if requests.Load() != 1 {
				t.Fatalf("request count = %d, want exactly 1", requests.Load())
			}
		})
	}
}

func TestZoneDeleteRequiresExplicitConfirmationWithoutRequest(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})}
	t.Setenv("DANS_ENDPOINT", "http://dans.invalid/api/v1")
	t.Setenv("DANS_API_TOKEN", newTestToken(t))
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"zones", "delete", "example.org."}, Options{
		Version:    "test",
		Streams:    Streams{Out: &stdout, Err: &stderr},
		HTTPClient: client,
	})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if requests.Load() != 0 {
		t.Fatalf("unconfirmed deletion made %d requests", requests.Load())
	}
}
