package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ncode/dans/internal/httpapi"
)

func TestMaintenanceCommandSurfaceUsesOneExecutable(t *testing.T) {
	root := NewCommand(Options{Version: "test", Streams: discardStreams(), Maintenance: &fakeMaintenance{}})
	for _, path := range [][]string{
		{"db", "migrate"}, {"db", "status"}, {"bootstrap"},
		{"recover", "operator-token"}, {"restore", "finalize"},
	} {
		if commandAt(root, path...) == nil {
			t.Errorf("missing command %q", path)
		}
	}
}

func TestBootstrapUsesDatabaseSecretAndPrintsOneTimeTokenOnce(t *testing.T) {
	maintenance := &fakeMaintenance{
		credential: OneTimeCredential{
			IdentityID: "11111111-1111-4111-8111-111111111111",
			Handle:     "alice",
			TokenID:    "22222222-2222-4222-8222-222222222222",
			TokenLabel: "initial",
			Secret:     "one-time-secret",
		},
	}
	t.Setenv("DANS_DATABASE_URL", "postgres://dans@database/dans")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{
		"--output", "json", "bootstrap", "--handle", "alice", "--display-name", "Alice", "--token-label", "initial",
	}, Options{Version: "test", Streams: Streams{Out: &stdout, Err: &stderr}, Maintenance: maintenance})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if maintenance.bootstrapCalls != 1 {
		t.Fatalf("bootstrap calls = %d, want 1", maintenance.bootstrapCalls)
	}
	if maintenance.databaseURL.Value() != "postgres://dans@database/dans" {
		t.Fatal("database secret was not passed to maintenance adapter")
	}
	if got := strings.Count(stdout.String(), maintenance.credential.Secret); got != 1 {
		t.Fatalf("one-time secret appears %d times on stdout", got)
	}
	if strings.Contains(stderr.String(), maintenance.credential.Secret) {
		t.Fatal("one-time secret appeared on stderr")
	}
}

func TestRestoreFinalizationRequiresConfirmationBeforeMaintenance(t *testing.T) {
	maintenance := &fakeMaintenance{}
	t.Setenv("DANS_DATABASE_URL", "postgres://dans@database/dans")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{
		"restore", "finalize", "--identity-id", "11111111-1111-4111-8111-111111111111", "--token-label", "restored",
	}, Options{Version: "test", Streams: Streams{Out: &stdout, Err: &stderr}, Maintenance: maintenance})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if maintenance.finalizeCalls != 0 {
		t.Fatalf("unconfirmed finalization made %d calls", maintenance.finalizeCalls)
	}
}

func TestMaintenanceFailureIsRuntimeWithoutUsage(t *testing.T) {
	const databaseURL = "postgres://dans:database-password@database/dans"
	maintenance := &fakeMaintenance{err: errors.New("connect " + databaseURL + ": password database-password rejected")}
	t.Setenv("DANS_DATABASE_URL", databaseURL)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"db", "status"}, Options{
		Version: "test", Streams: Streams{Out: &stdout, Err: &stderr}, Maintenance: maintenance,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("runtime failure included usage: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), databaseURL) || strings.Contains(stderr.String(), "database-password") {
		t.Fatalf("runtime failure exposed database credentials: %q", stderr.String())
	}
}

func TestMaintenanceCancellationUsesExitCode130(t *testing.T) {
	t.Setenv("DANS_DATABASE_URL", "postgres://dans@database/dans")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"db", "status"}, Options{
		Version:     "test",
		Streams:     Streams{Out: &stdout, Err: &stderr},
		Maintenance: &fakeMaintenance{err: context.Canceled},
	})
	if code != 130 {
		t.Fatalf("exit code = %d, want 130; stderr = %q", code, stderr.String())
	}
}

type fakeMaintenance struct {
	status         MigrationState
	credential     OneTimeCredential
	err            error
	databaseURL    httpapi.Secret
	bootstrapCalls int
	finalizeCalls  int
}

func (fake *fakeMaintenance) Migrate(_ context.Context, databaseURL httpapi.Secret) error {
	fake.databaseURL = databaseURL
	return fake.err
}

func (fake *fakeMaintenance) MigrationStatus(_ context.Context, databaseURL httpapi.Secret) (MigrationState, error) {
	fake.databaseURL = databaseURL
	return fake.status, fake.err
}

func (fake *fakeMaintenance) Bootstrap(_ context.Context, databaseURL httpapi.Secret, _ BootstrapInput) (OneTimeCredential, error) {
	fake.databaseURL = databaseURL
	fake.bootstrapCalls++
	return fake.credential, fake.err
}

func (fake *fakeMaintenance) RecoverOperator(_ context.Context, databaseURL httpapi.Secret, _ OperatorRecoveryInput) (OneTimeCredential, error) {
	fake.databaseURL = databaseURL
	return fake.credential, fake.err
}

func (fake *fakeMaintenance) FinalizeRestore(_ context.Context, databaseURL httpapi.Secret, _ RestoreFinalizationInput) (OneTimeCredential, error) {
	fake.databaseURL = databaseURL
	fake.finalizeCalls++
	return fake.credential, fake.err
}
