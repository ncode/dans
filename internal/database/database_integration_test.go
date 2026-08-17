//go:build integration

package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const testDatabaseURL = "DANS_TEST_DATABASE_URL"

func TestMigrate_concurrentCallersSerialize(t *testing.T) {
	t.Parallel()

	conn, schema, dsn := newTestSchema(t)
	second := connectToTestSchema(t, dsn, schema)

	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	result := make(chan error, 2)
	for _, migrationConn := range []*pgx.Conn{conn, second} {
		go func() {
			ready <- struct{}{}
			<-start
			result <- Migrate(t.Context(), migrationConn)
		}()
	}
	<-ready
	<-ready
	close(start)

	for range 2 {
		if err := <-result; err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}
	}

	var count int
	if err := conn.QueryRow(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("schema_migrations rows = %d, want 1", count)
	}
}

func TestInitialSchema_constraints(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	t.Run("single installation", func(t *testing.T) {
		_, err := conn.Exec(t.Context(), `
			INSERT INTO installation_metadata (singleton, installation_id)
			VALUES (true, '10000000-0000-4000-8000-000000000001')`)
		if err != nil {
			t.Fatalf("insert installation: %v", err)
		}
		_, err = conn.Exec(t.Context(), `
			INSERT INTO installation_metadata (singleton, installation_id)
			VALUES (true, '10000000-0000-4000-8000-000000000002')`)
		requireSQLState(t, err, "23505")
	})

	t.Run("identity and group handles stay unique", func(t *testing.T) {
		insertIdentity(t, conn, "10000000-0000-4000-8000-000000000010", "alice")
		_, err := conn.Exec(t.Context(), `
			INSERT INTO identities (id, kind, handle)
			VALUES ('10000000-0000-4000-8000-000000000011', 'service', 'alice')`)
		requireSQLState(t, err, "23505")

		_, err = conn.Exec(t.Context(), `
			INSERT INTO groups (id, handle)
			VALUES
				('20000000-0000-4000-8000-000000000001', 'operators'),
				('20000000-0000-4000-8000-000000000002', 'operators')`)
		requireSQLState(t, err, "23505")
	})

	t.Run("foreign keys reject missing resources", func(t *testing.T) {
		_, err := conn.Exec(t.Context(), `
			INSERT INTO group_memberships (group_id, identity_id, added_by_identity_id)
			VALUES (
				'20000000-0000-4000-8000-000000000099',
				'10000000-0000-4000-8000-000000000099',
				'10000000-0000-4000-8000-000000000010'
			)`)
		requireSQLState(t, err, "23503")
	})

	t.Run("token lifecycle and active label", func(t *testing.T) {
		_, err := conn.Exec(t.Context(), `
			INSERT INTO api_tokens (
				id, identity_id, label, digest, created_at, expires_at
			) VALUES (
				'30000000-0000-4000-8000-000000000001',
				'10000000-0000-4000-8000-000000000010',
				'build', decode(repeat('01', 32), 'hex'),
				'2026-01-02T00:00:00Z', '2026-01-01T00:00:00Z'
			)`)
		requireSQLState(t, err, "23514")

		_, err = conn.Exec(t.Context(), `
			INSERT INTO api_tokens (id, identity_id, label, digest)
			VALUES (
				'30000000-0000-4000-8000-000000000002',
				'10000000-0000-4000-8000-000000000010',
				'build', decode(repeat('02', 32), 'hex')
			)`)
		if err != nil {
			t.Fatalf("insert token: %v", err)
		}
		_, err = conn.Exec(t.Context(), `
			INSERT INTO api_tokens (id, identity_id, label, digest)
			VALUES (
				'30000000-0000-4000-8000-000000000003',
				'10000000-0000-4000-8000-000000000010',
				'build', decode(repeat('03', 32), 'hex')
			)`)
		requireSQLState(t, err, "23505")

		if _, err := conn.Exec(t.Context(), `
			UPDATE api_tokens SET revoked_at = now()
			WHERE id = '30000000-0000-4000-8000-000000000002'`); err != nil {
			t.Fatalf("revoke token: %v", err)
		}
		if _, err := conn.Exec(t.Context(), `
			INSERT INTO api_tokens (id, identity_id, label, digest)
			VALUES (
				'30000000-0000-4000-8000-000000000003',
				'10000000-0000-4000-8000-000000000010',
				'build', decode(repeat('03', 32), 'hex')
			)`); err != nil {
			t.Fatalf("reuse revoked token label: %v", err)
		}
	})

	t.Run("binding and delegation lifecycle", func(t *testing.T) {
		_, err := conn.Exec(t.Context(), `
			INSERT INTO zone_bindings (
				id, generation, upstream, powerdns_zone_id, zone_name, created_by_identity_id
			) VALUES (
				'40000000-0000-4000-8000-000000000001',
				1,
				'default', 'example.com.', 'example.com.',
				'10000000-0000-4000-8000-000000000010'
			)`)
		if err != nil {
			t.Fatalf("insert binding: %v", err)
		}
		_, err = conn.Exec(t.Context(), `
			INSERT INTO zone_bindings (
				id, generation, upstream, powerdns_zone_id, zone_name, created_by_identity_id
			) VALUES (
				'40000000-0000-4000-8000-000000000002',
				2,
				'default', 'example.com.', 'example.com.',
				'10000000-0000-4000-8000-000000000010'
			)`)
		requireSQLState(t, err, "23505")

		if _, err := conn.Exec(t.Context(), `
			UPDATE zone_bindings
			SET retired_at = now(), retired_by_identity_id = '10000000-0000-4000-8000-000000000010'
			WHERE id = '40000000-0000-4000-8000-000000000001'`); err != nil {
			t.Fatalf("retire binding: %v", err)
		}
		if _, err := conn.Exec(t.Context(), `
			INSERT INTO zone_bindings (
				id, generation, upstream, powerdns_zone_id, zone_name, created_by_identity_id
			) VALUES (
				'40000000-0000-4000-8000-000000000002',
				2,
				'default', 'example.com.', 'example.com.',
				'10000000-0000-4000-8000-000000000010'
			)`); err != nil {
			t.Fatalf("insert next binding generation: %v", err)
		}

		_, err = conn.Exec(t.Context(), `
			INSERT INTO delegations (
				id, zone_binding_id, grantee_identity_id, grantee_group_id,
				created_by_identity_id
			) VALUES (
				'50000000-0000-4000-8000-000000000001',
				'40000000-0000-4000-8000-000000000002',
				'10000000-0000-4000-8000-000000000010',
				'20000000-0000-4000-8000-000000000001',
				'10000000-0000-4000-8000-000000000010'
			)`)
		requireSQLState(t, err, "23514")
	})

	t.Run("audit operation has one intent and outcome", func(t *testing.T) {
		insertAuditIntent(t, conn, "60000000-0000-4000-8000-000000000001", "70000000-0000-4000-8000-000000000001")
		_, err := conn.Exec(t.Context(), auditIntentSQL,
			"60000000-0000-4000-8000-000000000002",
			"70000000-0000-4000-8000-000000000001")
		requireSQLState(t, err, "23505")

		if _, err := conn.Exec(t.Context(), auditOutcomeSQL,
			"60000000-0000-4000-8000-000000000003",
			"70000000-0000-4000-8000-000000000001",
			"60000000-0000-4000-8000-000000000001"); err != nil {
			t.Fatalf("insert audit outcome: %v", err)
		}
		_, err = conn.Exec(t.Context(), auditOutcomeSQL,
			"60000000-0000-4000-8000-000000000004",
			"70000000-0000-4000-8000-000000000001",
			"60000000-0000-4000-8000-000000000001")
		requireSQLState(t, err, "23505")
	})
}

func TestRuntimePrivileges_keepAuditAppendOnly(t *testing.T) {
	conn, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	role := "dans_runtime_test_" + randomSuffix(t)
	if _, err := conn.Exec(t.Context(), "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" NOLOGIN"); err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "42501" {
			t.Skipf("database user cannot create the temporary runtime role: %v", err)
		}
		t.Fatalf("create runtime role: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = conn.Exec(ctx, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize())
		_, _ = conn.Exec(ctx, "DROP ROLE "+pgx.Identifier{role}.Sanitize())
	})
	grantRuntimeForTest(t, conn, schema, role)

	insertAuditIntent(t, conn, "60000000-0000-4000-8000-000000000010", "70000000-0000-4000-8000-000000000010")
	runtimeConn := connectToTestSchema(t, dsn, schema)
	if _, err := runtimeConn.Exec(t.Context(), "SET ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatalf("set runtime role: %v", err)
	}

	if _, err := runtimeConn.Exec(t.Context(), `
		INSERT INTO audit_events (
			id, event_kind, request_id, action, target_kind, target_id, result
		) VALUES (
			'60000000-0000-4000-8000-000000000011',
			'management', 'request-runtime', 'identity.create',
			'identity', '10000000-0000-4000-8000-000000000010', 'succeeded'
		)`); err != nil {
		t.Fatalf("runtime append audit event: %v", err)
	}

	statements := []string{
		"UPDATE audit_events SET action = 'changed' WHERE id = '60000000-0000-4000-8000-000000000010'",
		"DELETE FROM audit_events WHERE id = '60000000-0000-4000-8000-000000000010'",
		"TRUNCATE audit_events",
		"INSERT INTO schema_migrations (version, name, checksum) VALUES (99, 'forbidden', 'forbidden')",
		"CREATE TABLE runtime_must_not_create_tables (id integer)",
	}
	for _, statement := range statements {
		_, err := runtimeConn.Exec(t.Context(), statement)
		requireSQLState(t, err, "42501")
	}
}

const auditIntentSQL = `
	INSERT INTO audit_events (
		id, event_kind, operation_id, request_id, action, target_kind,
		target_id, result, deadline_at
	) VALUES ($1, 'dns_intent', $2, 'request-intent', 'zone.patch',
		'zone_binding', '40000000-0000-4000-8000-000000000002',
		'pending', now() + interval '30 seconds')`

const auditOutcomeSQL = `
	INSERT INTO audit_events (
		id, event_kind, operation_id, intent_event_id, intent_event_kind,
		request_id, action, target_kind, target_id, result
	) VALUES ($1, 'dns_outcome', $2, $3, 'dns_intent', 'request-outcome',
		'zone.patch', 'zone_binding',
		'40000000-0000-4000-8000-000000000002', 'succeeded')`

func newTestSchema(t testing.TB) (*pgx.Conn, string, string) {
	t.Helper()

	dsn := os.Getenv(testDatabaseURL)
	if dsn == "" {
		t.Skipf("%s is not set", testDatabaseURL)
	}
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	schema := "dans_test_" + randomSuffix(t)
	if _, err := conn.Exec(t.Context(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		_ = conn.Close(t.Context())
		t.Fatalf("create test schema: %v", err)
	}
	setSearchPath(t, conn, schema)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = conn.Exec(ctx, "RESET ROLE")
		_, _ = conn.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		_ = conn.Close(ctx)
	})
	return conn, schema, dsn
}

func connectToTestSchema(t *testing.T, dsn, schema string) *pgx.Conn {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	setSearchPath(t, conn, schema)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = conn.Close(ctx)
	})
	return conn
}

func setSearchPath(t testing.TB, conn *pgx.Conn, schema string) {
	t.Helper()

	if _, err := conn.Exec(t.Context(), "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
}

func randomSuffix(t testing.TB) string {
	t.Helper()

	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		t.Fatalf("generate test suffix: %v", err)
	}
	return hex.EncodeToString(data[:])
}

func requireSQLState(t *testing.T, err error, want string) {
	t.Helper()

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		t.Fatalf("error = %v, want PostgreSQL SQLSTATE %s", err, want)
	}
	if pgErr.Code != want {
		t.Fatalf("SQLSTATE = %s (%v), want %s", pgErr.Code, err, want)
	}
}

func insertIdentity(t *testing.T, conn *pgx.Conn, id, handle string) {
	t.Helper()

	if _, err := conn.Exec(t.Context(), `
		INSERT INTO identities (id, kind, handle)
		VALUES ($1, 'user', $2)`, id, handle); err != nil {
		t.Fatalf("insert identity %q: %v", handle, err)
	}
}

func insertAuditIntent(t *testing.T, conn *pgx.Conn, id, operationID string) {
	t.Helper()

	if _, err := conn.Exec(t.Context(), auditIntentSQL, id, operationID); err != nil {
		t.Fatalf("insert audit intent: %v", err)
	}
}

func grantRuntimeForTest(t *testing.T, conn *pgx.Conn, schema, role string) {
	t.Helper()

	s := pgx.Identifier{schema}.Sanitize()
	r := pgx.Identifier{role}.Sanitize()
	statements := []string{
		fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", s, r),
		fmt.Sprintf("GRANT SELECT ON %s.schema_migrations TO %s", s, r),
		fmt.Sprintf("GRANT SELECT, INSERT ON %s.audit_events TO %s", s, r),
	}
	for _, statement := range statements {
		if _, err := conn.Exec(t.Context(), statement); err != nil {
			t.Fatalf("apply test runtime grant %q: %v", statement, err)
		}
	}
}
