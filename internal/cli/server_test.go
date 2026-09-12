package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ncode/dans/internal/httpapi"
)

func TestServeCommandFreezesConfigurationAndSecretsBeforeRunning(t *testing.T) {
	t.Setenv("DANS_DATABASE_URL", "postgres://dans@database/dans")
	t.Setenv("DANS_POWERDNS_API_KEY", "powerdns-secret")
	server := &fakeServer{}
	var stdout, stderr bytes.Buffer

	code := Execute(context.Background(), []string{
		"--powerdns-url", "http://127.0.0.1:8081",
		"--request-timeout", "9s",
		"--database-max-connections", "15",
		"serve",
	}, Options{
		Version: "test",
		Streams: Streams{Out: &stdout, Err: &stderr},
		Server:  server,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if server.calls != 1 {
		t.Fatalf("serve calls = %d, want 1", server.calls)
	}
	if server.config.Address != defaultListen || server.config.PowerDNSURL != "http://127.0.0.1:8081" {
		t.Fatalf("serve addresses = %#v", server.config)
	}
	if server.config.RequestTimeout != 9*time.Second || server.config.DatabaseMaxConnections != 15 {
		t.Fatalf("serve limits = %#v", server.config)
	}
	if server.config.DatabaseURL.Value() != "postgres://dans@database/dans" || server.config.PowerDNSAPIKey.Value() != "powerdns-secret" {
		t.Fatal("serve did not receive the resolved secrets")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q; want empty", stdout.String(), stderr.String())
	}
}

func TestServeRejectsAmbiguousPowerDNSTransportBeforeRunning(t *testing.T) {
	t.Setenv("DANS_DATABASE_URL", "postgres://dans@database/dans")
	t.Setenv("DANS_POWERDNS_API_KEY", "powerdns-secret")
	server := &fakeServer{}
	var stderr bytes.Buffer

	code := Execute(context.Background(), []string{
		"--powerdns-url", "http://127.0.0.1:8081",
		"--powerdns-unix-socket", "/run/powerdns/pdns.sock",
		"serve",
	}, Options{Version: "test", Streams: Streams{Err: &stderr}, Server: server})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %q", code, stderr.String())
	}
	if server.calls != 0 {
		t.Fatalf("invalid configuration made %d serve calls", server.calls)
	}
}

func TestServeCancellationUsesExitCode130(t *testing.T) {
	t.Setenv("DANS_DATABASE_URL", "postgres://dans@database/dans")
	t.Setenv("DANS_POWERDNS_API_KEY", "powerdns-secret")
	var stderr bytes.Buffer

	code := Execute(context.Background(), []string{
		"--powerdns-url", "http://127.0.0.1:8081", "serve",
	}, Options{
		Version: "test",
		Streams: Streams{Err: &stderr},
		Server:  &fakeServer{err: context.Canceled},
	})
	if code != 130 {
		t.Fatalf("exit code = %d, want 130; stderr = %q", code, stderr.String())
	}
	if bytes.Contains(stderr.Bytes(), []byte("powerdns-secret")) {
		t.Fatalf("stderr exposed upstream key: %q", stderr.String())
	}
}

func TestServeRuntimeFailureIsOneStructuredRedactedErrorLog(t *testing.T) {
	t.Setenv("DANS_DATABASE_URL", "postgres://dans:database-secret@database/dans")
	t.Setenv("DANS_POWERDNS_API_KEY", "powerdns-secret")
	var stderr bytes.Buffer

	code := Execute(context.Background(), []string{
		"--powerdns-url", "http://127.0.0.1:8081", "serve",
	}, Options{
		Version: "test", Streams: Streams{Err: &stderr},
		Server: &fakeServer{err: fmt.Errorf("dial failed with database-secret and powerdns-secret")},
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	lines := bytes.Split(bytes.TrimSpace(stderr.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("stderr lines = %d, want 1: %q", len(lines), stderr.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(lines[0], &entry); err != nil {
		t.Fatalf("runtime log is not JSON: %v: %q", err, lines[0])
	}
	if entry["level"] != "ERROR" || entry["msg"] != "serve failed" || entry["event"] != "runtime_error" || entry["error"] == "" {
		t.Errorf("runtime log = %#v", entry)
	}
	if bytes.Contains(stderr.Bytes(), []byte("database-secret")) || bytes.Contains(stderr.Bytes(), []byte("powerdns-secret")) {
		t.Fatalf("runtime log exposed a secret: %q", stderr.String())
	}
}

func TestServeConfigDiagnosticsRedactNestedSecrets(t *testing.T) {
	t.Parallel()

	config := ServeConfig{
		DatabaseURL:    httpapi.NewSecret("postgres://dans:database-secret@database/dans"),
		PowerDNSAPIKey: httpapi.NewSecret("powerdns-secret"),
	}
	for _, diagnostic := range []string{fmt.Sprintf("%v", config), fmt.Sprintf("%+v", config), fmt.Sprintf("%#v", config)} {
		if bytes.Contains([]byte(diagnostic), []byte("database-secret")) || bytes.Contains([]byte(diagnostic), []byte("powerdns-secret")) {
			t.Fatalf("ServeConfig diagnostic exposed a secret: %s", diagnostic)
		}
	}
}

func TestServeReadsFileBackedSecretsOnceBeforeRuntime(t *testing.T) {
	dir := t.TempDir()
	databaseFile := filepath.Join(dir, "database-url")
	powerDNSFile := filepath.Join(dir, "powerdns-key")
	if err := os.WriteFile(databaseFile, []byte("postgres://dans@database/initial\n"), 0o600); err != nil {
		t.Fatalf("write database secret: %v", err)
	}
	if err := os.WriteFile(powerDNSFile, []byte("initial-key\n"), 0o600); err != nil {
		t.Fatalf("write PowerDNS secret: %v", err)
	}
	configFile := writeConfigFile(t, dir, "serve.json", fmt.Sprintf(
		`{"database_url_file":%q,"powerdns_api_key_file":%q,"powerdns_url":"http://127.0.0.1:8081"}`,
		databaseFile, powerDNSFile,
	))
	server := &fakeServer{run: func(config ServeConfig) error {
		if err := os.WriteFile(databaseFile, []byte("postgres://dans@database/changed\n"), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(powerDNSFile, []byte("changed-key\n"), 0o600); err != nil {
			return err
		}
		if config.DatabaseURL.Value() != "postgres://dans@database/initial" || config.PowerDNSAPIKey.Value() != "initial-key" {
			return fmt.Errorf("runtime configuration changed after startup")
		}
		return nil
	}}
	var stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--config", configFile, "serve"}, Options{
		Version: "test", Streams: Streams{Err: &stderr}, Server: server,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

type fakeServer struct {
	calls  int
	config ServeConfig
	err    error
	run    func(ServeConfig) error
}

func TestServeBrowserCookieModeRequiresExplicitDevelopmentSetting(t *testing.T) {
	t.Setenv("DANS_DATABASE_URL", "postgres://dans@database/dans")
	t.Setenv("DANS_POWERDNS_API_KEY", "powerdns-secret")
	for _, tc := range []struct {
		mode        string
		flags       []string
		development bool
		code        int
	}{
		{code: 0},
		{mode: "development-http", development: true},
		{mode: "development-http", flags: []string{"--browser-cookie-mode", "secure"}},
		{mode: "invalid", code: 2},
	} {
		t.Setenv("DANS_BROWSER_COOKIE_MODE", tc.mode)
		server := &fakeServer{}
		args := append([]string{"--powerdns-url", "http://127.0.0.1:8081"}, tc.flags...)
		code := Execute(t.Context(), append(args, "serve"), Options{Server: server})
		if code != tc.code || server.config.DevelopmentHTTP != tc.development || (tc.code != 0 && server.calls != 0) {
			t.Errorf("cookie mode %q: exit=%d development=%v calls=%d", tc.mode, code, server.config.DevelopmentHTTP, server.calls)
		}
	}
}

func (server *fakeServer) Serve(_ context.Context, config ServeConfig) error {
	server.calls++
	server.config = config
	if server.run != nil {
		return server.run(config)
	}
	return server.err
}
