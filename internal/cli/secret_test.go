package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncode/dans/internal/identifier"
)

func TestLoadAPITokenAcceptsOneEnvironmentOrFileSource(t *testing.T) {
	token, _, err := identifier.NewToken()
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	t.Setenv("DANS_API_TOKEN", token)
	secret, err := loadAPIToken(Config{})
	if err != nil {
		t.Fatalf("load environment token: %v", err)
	}
	if secret.Value() != token {
		t.Fatal("environment token was not preserved")
	}
	if strings.Contains(secret.String(), token) {
		t.Fatal("formatted secret exposed the token")
	}
	t.Setenv("DANS_API_TOKEN", token+"\n")
	if _, err := loadAPIToken(Config{}); err == nil {
		t.Fatal("environment token with a terminal newline succeeded")
	}

	t.Setenv("DANS_API_TOKEN", "")
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token+"\r\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	secret, err = loadAPIToken(Config{APITokenFile: path})
	if err != nil {
		t.Fatalf("load file token: %v", err)
	}
	if secret.Value() != token {
		t.Fatal("file token did not have exactly one CRLF removed")
	}

	symlink := filepath.Join(t.TempDir(), "mounted-token")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatalf("create token symlink: %v", err)
	}
	if _, err := loadAPIToken(Config{APITokenFile: symlink}); err != nil {
		t.Fatalf("load symlinked token: %v", err)
	}
}

func TestLoadAPITokenRejectsConflictsAndMalformedSourcesWithoutDisclosure(t *testing.T) {
	token, _, err := identifier.NewToken()
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	dir := t.TempDir()
	validPath := filepath.Join(dir, "valid")
	if err := os.WriteFile(validPath, []byte(token), 0o600); err != nil {
		t.Fatalf("write valid token: %v", err)
	}
	t.Setenv("DANS_API_TOKEN", token)
	if _, err := loadAPIToken(Config{APITokenFile: validPath}); err == nil {
		t.Fatal("conflicting environment and file sources succeeded")
	} else if strings.Contains(err.Error(), token) {
		t.Fatal("conflict error disclosed the token")
	}
	t.Setenv("DANS_API_TOKEN", "")

	tests := []struct {
		name  string
		value []byte
	}{
		{name: "empty"},
		{name: "nul", value: append([]byte(token), 0)},
		{name: "two newlines", value: []byte(token + "\n\n")},
		{name: "invalid token", value: []byte("not-a-dans-token")},
		{name: "too large", value: bytes.Repeat([]byte("x"), maxSecretBytes+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "-"))
			if err := os.WriteFile(path, tt.value, 0o600); err != nil {
				t.Fatalf("write secret: %v", err)
			}
			if _, err := loadAPIToken(Config{APITokenFile: path}); err == nil {
				t.Fatal("malformed secret succeeded")
			}
		})
	}
}

func TestLiteralSecretFlagsAndJSONAreRejected(t *testing.T) {
	const literal = "must-not-be-printed"
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--api-token=" + literal, "version"}, Options{
		Version: "test",
		Streams: Streams{Out: &stdout, Err: &stderr},
	})
	if code != 2 {
		t.Fatalf("literal secret flag exit code = %d, want 2", code)
	}
	if strings.Contains(stderr.String(), literal) {
		t.Fatalf("literal secret was echoed: %q", stderr.String())
	}

	path := writeConfigFile(t, t.TempDir(), "literal.json", `{"api_token":"`+literal+`"}`)
	if _, err := loadConfig(configTestCommand(t, "--config", path), configScopeOnline); err == nil {
		t.Fatal("literal JSON secret succeeded")
	} else if strings.Contains(err.Error(), literal) {
		t.Fatal("literal JSON secret was echoed")
	}
}

func TestLoadPowerDNSKeyAcceptsOneRenderedAssignment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pdns.conf")
	if err := os.WriteFile(path, []byte("api=yes\napi-key = pdns-secret\nwebserver=yes\n"), 0o600); err != nil {
		t.Fatalf("write PowerDNS config: %v", err)
	}

	secret, err := loadPowerDNSAPIKey(Config{PowerDNSConfigFile: path})
	if err != nil {
		t.Fatalf("load PowerDNS key: %v", err)
	}
	if secret.Value() != "pdns-secret" {
		t.Fatal("PowerDNS key assignment was not parsed")
	}

	for name, contents := range map[string]string{
		"missing":  "api=yes\n",
		"multiple": "api-key=one\napi-key=two\n",
		"empty":    "api-key=\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".conf")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatalf("write PowerDNS config: %v", err)
			}
			if _, err := loadPowerDNSAPIKey(Config{PowerDNSConfigFile: path}); err == nil {
				t.Fatal("ambiguous rendered configuration succeeded")
			}
		})
	}
}

func TestLoadPowerDNSKeyRejectsCompetingSources(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	configPath := filepath.Join(dir, "pdns.conf")
	if err := os.WriteFile(keyPath, []byte("pdns-secret"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("api-key=pdns-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DANS_POWERDNS_API_KEY", "environment-secret")
	if _, err := loadPowerDNSAPIKey(Config{PowerDNSAPIKeyFile: keyPath}); err == nil {
		t.Fatal("environment and key file sources succeeded")
	}
	t.Setenv("DANS_POWERDNS_API_KEY", "")
	if _, err := loadPowerDNSAPIKey(Config{PowerDNSAPIKeyFile: keyPath, PowerDNSConfigFile: configPath}); err == nil {
		t.Fatal("key file and rendered config sources succeeded")
	}
}
