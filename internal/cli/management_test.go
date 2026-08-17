package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/identifier"
	"github.com/spf13/cobra"
)

func TestManagementCommandSurfaceCoversDANSWorkflows(t *testing.T) {
	root := NewCommand(Options{Version: "test", Streams: discardStreams()})
	paths := [][]string{
		{"identities", "list"}, {"identities", "create"}, {"identities", "get"}, {"identities", "update"},
		{"identities", "tokens", "list"}, {"identities", "tokens", "create"}, {"identities", "tokens", "revoke"},
		{"groups", "list"}, {"groups", "create"}, {"groups", "get"}, {"groups", "update"},
		{"groups", "members", "list"}, {"groups", "members", "add"}, {"groups", "members", "remove"},
		{"delegations", "list"}, {"delegations", "create"}, {"delegations", "get"}, {"delegations", "revoke"},
		{"bindings", "list"}, {"bindings", "create"}, {"bindings", "get"}, {"bindings", "observe"},
		{"bindings", "confirm-absent"}, {"bindings", "retry-delete"}, {"bindings", "rebind"},
		{"audit", "list"}, {"audit", "export"},
		{"me", "get"}, {"me", "groups"}, {"me", "delegations"}, {"me", "tokens"},
		{"me", "token-create"}, {"me", "token-revoke"},
	}

	for _, path := range paths {
		if commandAt(root, path...) == nil {
			t.Errorf("missing command %q", strings.Join(path, " "))
		}
	}
	if commandAt(root, "raw") != nil {
		t.Fatal("command tree exposes a raw-request escape hatch")
	}
	bindings := commandAt(root, "bindings")
	if bindings == nil || !contains(bindings.Aliases, "reconcile") {
		t.Fatal("zone-binding reconciliation has no CLI entry point")
	}
}

func TestPageFlagsAcceptContractMaximumAndRejectLargerLimit(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value   string
		wantErr bool
	}{
		{value: "500"},
		{value: "501", wantErr: true},
	} {
		command := &cobra.Command{Use: "list"}
		addPageFlags(command)
		if err := command.Flags().Parse([]string{"--limit", test.value}); err != nil {
			t.Fatalf("parse --limit %s: %v", test.value, err)
		}
		limit, _, err := pageFlags(command)
		if test.wantErr {
			if err == nil {
				t.Errorf("--limit %s accepted", test.value)
			}
			continue
		}
		if err != nil || limit == nil || int(*limit) != 500 {
			t.Errorf("--limit %s = %v, %v", test.value, limit, err)
		}
	}
}

func TestDecodeDelegationCreateFromStdin(t *testing.T) {
	command := &cobra.Command{}
	addDataFlag(command)
	command.SetIn(strings.NewReader("{\"zone_binding_id\":\"11111111-1111-4111-8111-111111111111\",\"identity_id\":\"22222222-2222-4222-8222-222222222222\",\"selectors\":[{\"kind\":\"glob\",\"value\":\"*.shared.example.test.\"}],\"record_types\":[\"A\"],\"change_kinds\":[\"REPLACE\",\"DELETE\",\"EXTEND\",\"PRUNE\"]}"))
	if err := command.Flags().Set("data", "-"); err != nil {
		t.Fatal(err)
	}

	if _, err := decodeData[api.DelegationCreate](command); err != nil {
		t.Fatalf("decodeData[DelegationCreate]() error = %v", err)
	}
}

func TestIdentityCreateUsesGeneratedClient(t *testing.T) {
	token := newTestToken(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/dans/identities" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Values("X-API-Key"); len(got) != 1 || got[0] != token {
			t.Errorf("X-API-Key = %q, want exactly one DANS token", got)
		}
		if got := request.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if got, want := string(body), "{\"handle\":\"alice\",\"kind\":\"user\"}"; got != want {
			t.Errorf("request body = %q, want %q", got, want)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{"created_at":"2026-08-14T10:00:00Z","display_name":"","enabled":true,"handle":"alice","id":"11111111-1111-4111-8111-111111111111","kind":"user","operator":false,"updated_at":"2026-08-14T10:00:00Z"}`)
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	bodyPath := writeConfigFile(t, dir, "identity.json", `{"handle":"alice","kind":"user"}`)
	t.Setenv("DANS_ENDPOINT", server.URL+"/api/v1")
	t.Setenv("DANS_API_TOKEN", token)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--output", "json", "identities", "create", "--data", bodyPath}, Options{
		Version:    "test",
		Streams:    Streams{Out: &stdout, Err: &stderr},
		HTTPClient: server.Client(),
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if requests.Load() != 1 {
		t.Fatalf("request count = %d, want 1", requests.Load())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode stdout: %v; stdout = %q", err, stdout.String())
	}
	if output["handle"] != "alice" {
		t.Fatalf("stdout handle = %#v", output["handle"])
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestTokenCreationPrintsOneTimeSecretOnlyToStdout(t *testing.T) {
	authToken := newTestToken(t)
	issuedToken := newTestToken(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/dans/me/tokens" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{"created_at":"2026-08-14T10:00:00Z","expires_at":"0001-01-01T00:00:00Z","id":"22222222-2222-4222-8222-222222222222","identity_id":"11111111-1111-4111-8111-111111111111","label":"qa","revoked_at":"0001-01-01T00:00:00Z","secret":"`+issuedToken+`","status":"active"}`)
	}))
	t.Cleanup(server.Close)

	bodyPath := writeConfigFile(t, t.TempDir(), "token.json", `{"label":"qa"}`)
	t.Setenv("DANS_ENDPOINT", server.URL+"/api/v1")
	t.Setenv("DANS_API_TOKEN", authToken)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--output", "json", "me", "token-create", "--data", bodyPath}, Options{
		Version:    "test",
		Streams:    Streams{Out: &stdout, Err: &stderr},
		HTTPClient: server.Client(),
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.Count(stdout.String(), issuedToken); got != 1 {
		t.Fatalf("issued token appears %d times on stdout", got)
	}
	if strings.Contains(stderr.String(), issuedToken) {
		t.Fatal("issued token appeared on stderr")
	}
}

func TestAPIFailureIsDiagnosticOnlyAndDoesNotEchoCredentialOrUsage(t *testing.T) {
	token := newTestToken(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, `{"error":"`+token+`"}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("DANS_ENDPOINT", server.URL+"/api/v1")
	t.Setenv("DANS_API_TOKEN", token)

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"me", "get"}, Options{
		Version:    "test",
		Streams:    Streams{Out: &stdout, Err: &stderr},
		HTTPClient: server.Client(),
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if strings.Contains(stderr.String(), token) {
		t.Fatal("API failure echoed the credential or response body")
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("runtime failure included usage: %q", stderr.String())
	}
}

func TestAuditExportPaginatesAsNDJSON(t *testing.T) {
	token := newTestToken(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := requests.Add(1)
		if request.URL.Query().Get("target_id") != "primary:example.org." {
			t.Errorf("target_id query = %q", request.URL.Query().Get("target_id"))
		}
		writer.Header().Set("Content-Type", "application/json")
		if call == 1 {
			_, _ = io.WriteString(writer, `{"items":[{"action":"create","actor_id":"11111111-1111-4111-8111-111111111111","details":{},"id":"22222222-2222-4222-8222-222222222222","occurred_at":"2026-08-14T10:00:00Z","request_id":"request-1","result":"success","target_id":"33333333-3333-4333-8333-333333333333","target_type":"identity"}],"next_cursor":"next"}`)
			return
		}
		if request.URL.Query().Get("cursor") != "next" {
			t.Errorf("second-page cursor = %q", request.URL.Query().Get("cursor"))
		}
		_, _ = io.WriteString(writer, `{"items":[{"action":"update","actor_id":"11111111-1111-4111-8111-111111111111","details":{},"id":"44444444-4444-4444-8444-444444444444","occurred_at":"2026-08-14T10:01:00Z","request_id":"request-2","result":"success","target_id":"33333333-3333-4333-8333-333333333333","target_type":"identity"}],"next_cursor":null}`)
	}))
	t.Cleanup(server.Close)

	t.Setenv("DANS_ENDPOINT", server.URL+"/api/v1")
	t.Setenv("DANS_API_TOKEN", token)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"audit", "export", "--target", "primary:example.org."}, Options{
		Version:    "test",
		Streams:    Streams{Out: &stdout, Err: &stderr},
		HTTPClient: server.Client(),
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("NDJSON line count = %d, stdout = %q", len(lines), stdout.String())
	}
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode NDJSON line %q: %v", line, err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("request count = %d, want 2", requests.Load())
	}
}

func TestOnlineCancellationStopsWithoutRetry(t *testing.T) {
	token := newTestToken(t)
	started := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)

	t.Setenv("DANS_ENDPOINT", server.URL+"/api/v1")
	t.Setenv("DANS_API_TOKEN", token)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan int, 1)
	go func() {
		result <- Execute(ctx, []string{"me", "get"}, Options{
			Version:    "test",
			Streams:    discardStreams(),
			HTTPClient: server.Client(),
		})
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	select {
	case code := <-result:
		if code != 130 {
			t.Fatalf("exit code = %d, want 130", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled command did not return")
	}
	if requests.Load() != 1 {
		t.Fatalf("request count = %d, want 1", requests.Load())
	}
}

func TestOfflineCommandsNeverUseHTTPClient(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, context.Canceled
	})}
	for _, args := range [][]string{{"help"}, {"version"}, {"completion", "bash"}} {
		if code := Execute(context.Background(), args, Options{Version: "test", Streams: discardStreams(), HTTPClient: client}); code != 0 {
			t.Fatalf("%v exit code = %d", args, code)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("offline commands made %d HTTP requests", requests.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func commandAt(root *cobra.Command, names ...string) *cobra.Command {
	current := root
	for _, name := range names {
		var next *cobra.Command
		for _, candidate := range current.Commands() {
			if candidate.Name() == name || contains(candidate.Aliases, name) {
				next = candidate
				break
			}
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func newTestToken(t *testing.T) string {
	t.Helper()
	token, _, err := identifier.NewToken()
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return token
}
