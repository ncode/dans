//go:build integration

package database

import (
	"errors"
	"testing"

	"github.com/ncode/dans/internal/identifier"
)

func TestMigrationStatusDoesNotMutateAndReportsCompatibility(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	status, err := MigrationStatus(t.Context(), conn)
	if err != nil {
		t.Fatalf("MigrationStatus(empty) error = %v", err)
	}
	if status.CurrentVersion != 0 || status.LatestVersion != 1 || !status.Compatible {
		t.Errorf("MigrationStatus(empty) = %+v", status)
	}
	var ledgerExists bool
	if err := conn.QueryRow(t.Context(), `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&ledgerExists); err != nil {
		t.Fatalf("check migration ledger: %v", err)
	}
	if ledgerExists {
		t.Error("MigrationStatus created the migration ledger")
	}

	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	status, err = MigrationStatus(t.Context(), conn)
	if err != nil || status.CurrentVersion != 1 || status.LatestVersion != 1 || !status.Compatible {
		t.Fatalf("MigrationStatus(current) = %+v, %v", status, err)
	}
	if _, err := conn.Exec(t.Context(), `UPDATE schema_migrations SET checksum = repeat('0', 64)`); err != nil {
		t.Fatalf("corrupt migration checksum: %v", err)
	}
	status, err = MigrationStatus(t.Context(), conn)
	if err != nil || status.Compatible {
		t.Fatalf("MigrationStatus(drift) = %+v, %v, want incompatible", status, err)
	}
}

func TestStoreBootstrapRecoveryAndRestoreFinalization(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	bootstrap, err := store.Bootstrap(t.Context(), BootstrapInput{
		Handle: "first-operator", DisplayName: "First Operator", TokenLabel: "bootstrap",
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if bootstrap.InstallationID == "" || bootstrap.Identity.Kind != "user" || !bootstrap.Identity.Enabled || !bootstrap.Identity.IsOperator {
		t.Errorf("Bootstrap() = %+v", bootstrap)
	}
	if identifier.ValidateToken(bootstrap.Token.Secret) != nil {
		t.Errorf("Bootstrap() secret is invalid")
	}
	if authenticated, err := store.Authenticate(t.Context(), bootstrap.Token.Secret); err != nil || authenticated.IdentityID != bootstrap.Identity.ID {
		t.Fatalf("Authenticate(bootstrap) = %+v, %v", authenticated, err)
	}
	if _, err := store.Bootstrap(t.Context(), BootstrapInput{Handle: "second", TokenLabel: "bootstrap"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Bootstrap(second) error = %v, want %v", err, ErrConflict)
	}
	assertAuditCount(t, conn, "installation.bootstrap", bootstrap.Identity.ID, 1)

	recovered, err := store.RecoverOperator(t.Context(), OperatorRecoveryInput{
		Handle: "first-operator", TokenLabel: "recovery",
	})
	if err != nil {
		t.Fatalf("RecoverOperator() error = %v", err)
	}
	if recovered.Token.IdentityID != bootstrap.Identity.ID || identifier.ValidateToken(recovered.Secret) != nil {
		t.Errorf("RecoverOperator() = %+v", recovered)
	}
	assertAuditCount(t, conn, "operator.recover", recovered.Token.ID, 1)

	nonOperator := insertTestIdentity(t, conn, "215", "not-operator")
	if _, err := store.RecoverOperator(t.Context(), OperatorRecoveryInput{IdentityID: nonOperator.ID, TokenLabel: "bad"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("RecoverOperator(non-operator) error = %v, want %v", err, ErrConflict)
	}

	if _, err := conn.Exec(t.Context(), `UPDATE installation_metadata SET restore_required = true, restore_finalized_at = NULL`); err != nil {
		t.Fatalf("mark restore required: %v", err)
	}
	finalized, err := store.FinalizeRestore(t.Context(), RestoreFinalizationInput{
		IdentityID: bootstrap.Identity.ID, TokenLabel: "after-restore",
	})
	if err != nil {
		t.Fatalf("FinalizeRestore() error = %v", err)
	}
	for name, secret := range map[string]string{"bootstrap": bootstrap.Token.Secret, "recovery": recovered.Secret} {
		if _, err := store.Authenticate(t.Context(), secret); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("Authenticate(%s restored token) error = %v, want %v", name, err, ErrUnauthenticated)
		}
	}
	if authenticated, err := store.Authenticate(t.Context(), finalized.Secret); err != nil || authenticated.IdentityID != bootstrap.Identity.ID {
		t.Fatalf("Authenticate(finalized replacement) = %+v, %v", authenticated, err)
	}
	metadata, err := New(conn).GetInstallationMetadata(t.Context())
	if err != nil || metadata.RestoreRequired || !metadata.RestoreFinalizedAt.Valid {
		t.Fatalf("installation metadata after finalization = %+v, %v", metadata, err)
	}
	assertAuditCount(t, conn, "restore.finalize", finalized.Token.ID, 1)
}

func TestStoreConcurrentBootstrapCreatesOneInstallation(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	results := make([]BootstrapResult, 2)
	errs := runConcurrently(
		func() error {
			var err error
			results[0], err = NewStore(conn1).Bootstrap(t.Context(), BootstrapInput{Handle: "winner-a", TokenLabel: "initial"})
			return err
		},
		func() error {
			var err error
			results[1], err = NewStore(conn2).Bootstrap(t.Context(), BootstrapInput{Handle: "winner-b", TokenLabel: "initial"})
			return err
		},
	)
	assertOneSuccessOneConflict(t, "bootstrap", errs)
	var installations, operators, tokens int
	if err := conn1.QueryRow(t.Context(), `
		SELECT
			(SELECT count(*) FROM installation_metadata),
			(SELECT count(*) FROM identities WHERE enabled AND is_operator),
			(SELECT count(*) FROM api_tokens WHERE revoked_at IS NULL)`).Scan(&installations, &operators, &tokens); err != nil {
		t.Fatalf("count bootstrap resources: %v", err)
	}
	if installations != 1 || operators != 1 || tokens != 1 {
		t.Errorf("bootstrap counts = installations %d, operators %d, tokens %d; want 1 each", installations, operators, tokens)
	}
	for i, err := range errs {
		if errors.Is(err, ErrConflict) && results[i].Token.Secret != "" {
			t.Error("conflicting bootstrap returned a token secret")
		}
	}
}
