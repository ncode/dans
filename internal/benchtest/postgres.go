//go:build integration

// Package benchtest provides disposable relational fixtures shared only by
// integration benchmark test files. It does not import the database package.
package benchtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ncode/dans/internal/identifier"
)

const (
	Token        = identifier.TokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	IdentityID   = "10000000-0000-4000-8000-000000000001"
	TokenID      = "30000000-0000-4000-8000-000000000001"
	BindingID    = "40000000-0000-4000-8000-000000000001"
	DelegationID = "50000000-0000-4000-8000-000000000001"
)

// NewPostgres creates a fresh schema per leaf benchmark invocation. The returned
// DSN includes its search_path so every pooled connection uses that same schema.
func NewPostgres(t testing.TB) (*pgx.Conn, string) {
	t.Helper()
	dsn := os.Getenv("DANS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable test database is not configured")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid benchmark database configuration")
	}
	conn, err := pgx.ConnectConfig(t.Context(), config)
	if err != nil {
		t.Fatal("cannot connect to benchmark database")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		_ = conn.Close(t.Context())
		t.Fatal(err)
	}
	schema := "benchmark_" + hex.EncodeToString(suffix[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		_ = conn.Close(t.Context())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, resetErr := conn.Exec(ctx, "RESET search_path")
		_, dropErr := conn.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		closeErr := conn.Close(ctx)
		if resetErr != nil || dropErr != nil || closeErr != nil {
			t.Error("benchmark schema cleanup failed")
		}
	})
	Exec(t, conn, "SET search_path TO "+quoted)
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("invalid benchmark database URL")
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}
	return conn, dsn
}

func Exec(t testing.TB, conn *pgx.Conn, query string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(t.Context(), query, args...); err != nil {
		t.Fatalf("benchmark fixture SQL: %v", err)
	}
}

// Seed adds one direct wildcard grant and an initialized installation after the
// caller applies the real migrations. Audit events start empty.
func Seed(t testing.TB, conn *pgx.Conn) {
	t.Helper()
	digest := identifier.DigestToken(Token)
	Exec(t, conn, `INSERT INTO installation_metadata (installation_id) VALUES ('90000000-0000-4000-8000-000000000001')`)
	Exec(t, conn, `INSERT INTO identities (id, kind, handle) VALUES ($1, 'service', 'benchmark-writer')`, IdentityID)
	Exec(t, conn, `INSERT INTO api_tokens (id, identity_id, label, digest) VALUES ($1, $2, 'benchmark', $3)`, TokenID, IdentityID, digest[:])
	Exec(t, conn, `INSERT INTO zone_bindings (id, generation, upstream, powerdns_zone_id, zone_name, created_by_identity_id) VALUES ($1, 1, 'default', 'example.org.', 'example.org.', $2)`, BindingID, IdentityID)
	Exec(t, conn, `INSERT INTO delegations (id, zone_binding_id, grantee_identity_id, created_by_identity_id) VALUES ($1, $2, $3, $3)`, DelegationID, BindingID, IdentityID)
	Exec(t, conn, `INSERT INTO delegation_selectors (delegation_id, position, kind, selector, sql_like_pattern) VALUES ($1, 1, 'glob', '*.example.org.', '%.example.org.')`, DelegationID)
}

func AuditRows(t testing.TB, conn *pgx.Conn) int64 {
	t.Helper()
	var count int64
	if err := conn.QueryRow(t.Context(), "SELECT count(*) FROM audit_events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func CheckAuditRows(b *testing.B, conn *pgx.Conn, initial, added int64) {
	b.Helper()
	final := AuditRows(b, conn)
	if final != initial+added {
		b.Fatalf("audit rows = %d, want %d", final, initial+added)
	}
	b.ReportMetric(float64(initial), "initial-rows")
	b.ReportMetric(float64(final), "final-rows")
}
