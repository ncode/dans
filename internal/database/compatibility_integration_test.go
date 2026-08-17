//go:build integration

package database

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRuntimeCompatibilityAgainstPostgreSQL(t *testing.T) {
	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := conn.Exec(t.Context(), `
        INSERT INTO installation_metadata (installation_id)
        VALUES ('00000000-0000-4000-8000-000000000001')`); err != nil {
		t.Fatalf("initialize installation: %v", err)
	}
	if err := CheckRuntimeCompatibility(t.Context(), conn); err != nil {
		t.Fatalf("compatible runtime: %v", err)
	}

	t.Run("read-only transaction", func(t *testing.T) {
		tx, err := conn.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatalf("begin read-only: %v", err)
		}
		defer tx.Rollback(t.Context())
		if err := CheckRuntimeCompatibility(t.Context(), tx); !errors.Is(err, ErrReadOnlyDatabase) {
			t.Fatalf("CheckRuntimeCompatibility error = %v", err)
		}
	})

	t.Run("newer schema", func(t *testing.T) {
		if _, err := conn.Exec(t.Context(), `
            INSERT INTO schema_migrations (version, name, checksum)
            VALUES (99, 'future', repeat('f', 64))`); err != nil {
			t.Fatalf("insert future migration: %v", err)
		}
		if err := CheckRuntimeCompatibility(t.Context(), conn); !errors.Is(err, ErrIncompatibleSchema) {
			t.Fatalf("CheckRuntimeCompatibility error = %v", err)
		}
		if _, err := conn.Exec(t.Context(), "DELETE FROM schema_migrations WHERE version = 99"); err != nil {
			t.Fatalf("remove future migration: %v", err)
		}
	})

	t.Run("restore pending", func(t *testing.T) {
		if _, err := conn.Exec(t.Context(), "UPDATE installation_metadata SET restore_required = true"); err != nil {
			t.Fatalf("mark restore pending: %v", err)
		}
		if err := CheckRuntimeCompatibility(t.Context(), conn); !errors.Is(err, ErrInstallationUnavailable) {
			t.Fatalf("CheckRuntimeCompatibility error = %v", err)
		}
	})
}
