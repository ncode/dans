package upstream

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPClientValidatesTransportChoice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config TransportConfig
	}{
		{name: "missing transport", config: TransportConfig{Timeout: time.Second}},
		{name: "both transports", config: TransportConfig{URL: "http://127.0.0.1:8081", UnixSocket: "/run/pdns.sock", Timeout: time.Second}},
		{name: "bad scheme", config: TransportConfig{URL: "ftp://127.0.0.1", Timeout: time.Second}},
		{name: "credentials in URL", config: TransportConfig{URL: "http://user:pass@127.0.0.1", Timeout: time.Second}},
		{name: "URL path", config: TransportConfig{URL: "http://127.0.0.1/api", Timeout: time.Second}},
		{name: "relative socket", config: TransportConfig{UnixSocket: "pdns.sock", Timeout: time.Second}},
		{name: "zero timeout", config: TransportConfig{URL: "http://127.0.0.1", Timeout: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := NewHTTPClient(tt.config); err == nil {
				t.Errorf("NewHTTPClient(%+v) error = nil", tt.config)
			}
		})
	}
}

func TestNewHTTPClientConfiguresRestrictedHTTPTransport(t *testing.T) {
	t.Parallel()

	client, baseURL, err := NewHTTPClient(TransportConfig{
		URL:     "https://pdns.internal:8443/",
		Timeout: 7 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	if baseURL.String() != "https://pdns.internal:8443" {
		t.Errorf("base URL = %q", baseURL)
	}
	if client.Timeout != 7*time.Second {
		t.Errorf("client timeout = %s, want 7s", client.Timeout)
	}
	if client == http.DefaultClient {
		t.Error("NewHTTPClient returned http.DefaultClient")
	}
	if client.CheckRedirect == nil {
		t.Error("redirect policy is nil")
	}
	request := &http.Request{}
	if err := client.CheckRedirect(request, []*http.Request{{}}); err != http.ErrUseLastResponse {
		t.Errorf("CheckRedirect() error = %v, want http.ErrUseLastResponse", err)
	}
}

func TestUnixSocketTransport(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "dans-upstream-")
	if err != nil {
		t.Fatalf("create short socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "powerdns.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		<-done
	})

	client, baseURL, err := NewHTTPClient(TransportConfig{UnixSocket: socket, Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL.String()+"/ping", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET over unix socket: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
}

func TestLoadKeyReadsExactlyOneApprovedSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := os.WriteFile(keyFile, []byte("file-key\r\n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	symlink := filepath.Join(dir, "key-link")
	if err := os.Symlink(keyFile, symlink); err != nil {
		t.Fatalf("symlink key: %v", err)
	}
	fragment := filepath.Join(dir, "pdns.conf")
	if err := os.WriteFile(fragment, []byte("# rendered config\napi=yes\napi-key = fragment-key\nwebserver=yes\n"), 0o600); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	tests := []struct {
		name    string
		sources KeySources
		want    string
	}{
		{name: "environment", sources: KeySources{Environment: "env-key"}, want: "env-key"},
		{name: "file", sources: KeySources{File: keyFile}, want: "file-key"},
		{name: "symlink", sources: KeySources{File: symlink}, want: "file-key"},
		{name: "rendered fragment", sources: KeySources{RenderedFragment: fragment}, want: "fragment-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key, err := LoadKey(tt.sources)
			if err != nil {
				t.Fatalf("LoadKey: %v", err)
			}
			if key.Value() != tt.want {
				t.Errorf("key = %q, want %q", key.Value(), tt.want)
			}
			if strings.Contains(fmt.Sprint(key), tt.want) {
				t.Error("formatted key exposed plaintext")
			}
		})
	}
}

func TestLoadKeyRejectsMissingConflictingOrMalformedSources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, value string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	empty := write("empty", "\n")
	nul := write("nul", "key\x00value")
	twoLines := write("lines", "one\ntwo")
	tooLarge := write("large", strings.Repeat("x", MaxKeyBytes+1))
	missingAssignment := write("missing.conf", "api=yes\n")
	duplicateAssignment := write("duplicate.conf", "api-key=one\napi-key=two\n")
	emptyAssignment := write("empty.conf", "api-key=\n")
	directory := filepath.Join(dir, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tests := []struct {
		name    string
		sources KeySources
	}{
		{name: "missing", sources: KeySources{}},
		{name: "environment and file", sources: KeySources{Environment: "key", File: empty}},
		{name: "file and fragment", sources: KeySources{File: empty, RenderedFragment: missingAssignment}},
		{name: "empty environment", sources: KeySources{Environment: ""}},
		{name: "empty file", sources: KeySources{File: empty}},
		{name: "NUL file", sources: KeySources{File: nul}},
		{name: "multiple lines", sources: KeySources{File: twoLines}},
		{name: "oversized file", sources: KeySources{File: tooLarge}},
		{name: "directory", sources: KeySources{File: directory}},
		{name: "fragment without assignment", sources: KeySources{RenderedFragment: missingAssignment}},
		{name: "fragment duplicate assignment", sources: KeySources{RenderedFragment: duplicateAssignment}},
		{name: "fragment empty assignment", sources: KeySources{RenderedFragment: emptyAssignment}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := LoadKey(tt.sources); err == nil {
				t.Errorf("LoadKey(%+v) error = nil", tt.sources)
			}
		})
	}
}
