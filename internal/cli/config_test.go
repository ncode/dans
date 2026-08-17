package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ncode/dans/internal/httpapi"

	"github.com/spf13/cobra"
)

func TestLoadConfigUsesDefaultFileEnvironmentFlagPrecedence(t *testing.T) {
	dir := t.TempDir()
	envFile := writeConfigFile(t, dir, "env.json", `{"endpoint":"http://file-env.invalid"}`)
	flagFile := writeConfigFile(t, dir, "flag.json", `{"endpoint":"http://file-flag.invalid"}`)
	t.Setenv("DANS_CONFIG", envFile)
	t.Setenv("DANS_ENDPOINT", "http://environment.invalid")

	cmd := configTestCommand(t, "--config", flagFile, "--endpoint", "http://flag.invalid")
	config, err := loadConfig(cmd, configScopeOnline)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.Endpoint != "http://flag.invalid" {
		t.Fatalf("endpoint = %q, want flag value", config.Endpoint)
	}

	cmd = configTestCommand(t, "--config", flagFile)
	config, err = loadConfig(cmd, configScopeOnline)
	if err != nil {
		t.Fatalf("load config without endpoint flag: %v", err)
	}
	if config.Endpoint != "http://environment.invalid" {
		t.Fatalf("endpoint = %q, want environment value", config.Endpoint)
	}

	t.Setenv("DANS_ENDPOINT", "")
	cmd = configTestCommand(t, "--config", flagFile)
	config, err = loadConfig(cmd, configScopeOnline)
	if err != nil {
		t.Fatalf("load config without endpoint environment: %v", err)
	}
	if config.Endpoint != "http://file-flag.invalid" {
		t.Fatalf("endpoint = %q, want flag-selected file value", config.Endpoint)
	}
}

func TestLoadConfigDoesNotSearchOrBindUnknownEnvironment(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, ".dans.json", `{"unknown":true}`)
	t.Chdir(dir)
	t.Setenv("DANS_ENDPOINT_TYPO", "http://unexpected.invalid")

	config, err := loadConfig(configTestCommand(t), configScopeOnline)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.Endpoint != defaultEndpoint {
		t.Fatalf("endpoint = %q, want default %q", config.Endpoint, defaultEndpoint)
	}
}

func TestLoadConfigRejectsInvalidExplicitFiles(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
	}{
		{name: "missing", path: filepath.Join(dir, "missing.json")},
		{name: "malformed", path: writeConfigFile(t, dir, "malformed.json", `{"endpoint":`)},
		{name: "unknown", path: writeConfigFile(t, dir, "unknown.json", `{"endpoint":"http://dans.invalid","mystery":true}`)},
		{name: "duplicate", path: writeConfigFile(t, dir, "duplicate.json", `{"endpoint":"http://first.invalid","endpoint":"http://second.invalid"}`)},
		{name: "wrong type", path: writeConfigFile(t, dir, "wrong-type.json", `{"endpoint":42}`)},
		{name: "null", path: writeConfigFile(t, dir, "null.json", `{"endpoint":null}`)},
		{name: "not json", path: writeConfigFile(t, dir, "config.yaml", "endpoint: http://dans.invalid\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(configTestCommand(t, "--config", tt.path), configScopeOnline)
			if err == nil {
				t.Fatal("load config succeeded, want error")
			}
		})
	}
}

func TestConfigValidationIsCommandScoped(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "client.json", `{"endpoint":"http://dans.invalid"}`)
	cmd := configTestCommand(t, "--config", path)

	if _, err := loadConfig(cmd, configScopeOnline); err != nil {
		t.Fatalf("online config rejected unrelated missing settings: %v", err)
	}
	if _, err := loadConfig(configTestCommand(t, "--config", path), configScopeServe); err == nil {
		t.Fatal("serve config accepted without its server and PowerDNS settings")
	}
}

func TestOnlineConfigRejectsInvalidRequestTimeout(t *testing.T) {
	_, err := loadConfig(configTestCommand(t, "--request-timeout", "0s"), configScopeOnline)
	if err == nil || !strings.Contains(err.Error(), "request_timeout") {
		t.Fatalf("loadConfig error = %v, want request_timeout validation", err)
	}
}

func TestDatabaseConfigAllowsDedicatedEnvironmentSecret(t *testing.T) {
	t.Setenv("DANS_DATABASE_URL", "postgres://dans@database/dans")
	config, err := loadConfig(configTestCommand(t), configScopeDatabase)
	if err != nil {
		t.Fatalf("load database config: %v", err)
	}
	if _, err := loadDatabaseURL(config); err != nil {
		t.Fatalf("load database environment secret: %v", err)
	}
}

func TestServeConfigUsesStrictPrecedenceForBoundedSettings(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "serve.json", `{
		"powerdns_unix_socket":"/run/powerdns/pdns.sock",
		"request_timeout":"7s",
		"database_max_connections":"14"
	}`)
	t.Setenv("DANS_REQUEST_TIMEOUT", "8s")
	cmd := configTestCommand(t, "--config", path, "--request-timeout", "9s", "--database-max-connections", "15")

	config, err := loadConfig(cmd, configScopeServe)
	if err != nil {
		t.Fatalf("load serve config: %v", err)
	}
	serve, err := config.newServeConfig(httpapi.NewSecret("postgres://dans@database/dans"), httpapi.NewSecret("powerdns-key"))
	if err != nil {
		t.Fatalf("resolve serve config: %v", err)
	}
	if serve.PowerDNSUnixSocket != "/run/powerdns/pdns.sock" || serve.PowerDNSURL != "" {
		t.Fatalf("PowerDNS transport = %#v", serve)
	}
	if serve.RequestTimeout != 9*time.Second || serve.DatabaseMaxConnections != 15 {
		t.Fatalf("bounded settings = %#v", serve)
	}
}

func TestServeConfigRejectsInvalidBoundsBeforeRuntime(t *testing.T) {
	tests := []struct {
		name  string
		flag  string
		value string
		want  string
	}{
		{name: "duration", flag: "--request-timeout", value: "0s", want: "request_timeout"},
		{name: "body", flag: "--max-body-bytes", value: "67108865", want: "max_body_bytes"},
		{name: "pool", flag: "--database-max-connections", value: "3", want: "database_max_connections"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := configTestCommand(t, "--powerdns-url", "http://127.0.0.1:8081", test.flag, test.value)
			_, err := loadConfig(cmd, configScopeServe)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("loadConfig error = %v, want %q", err, test.want)
			}
		})
	}
}

func configTestCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := NewCommand(Options{Version: "test", Streams: discardStreams()})
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

func writeConfigFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
