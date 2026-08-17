//go:build integration

package database

import (
	"strconv"
	"testing"

	"github.com/ncode/dans/internal/identifier"
)

const (
	authorizationBenchmarkToken        = identifier.TokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	authorizationBenchmarkIdentityID   = "10000000-0000-4000-8000-000000000001"
	authorizationBenchmarkTokenID      = "30000000-0000-4000-8000-000000000001"
	authorizationBenchmarkBindingID    = "40000000-0000-4000-8000-000000000001"
	authorizationBenchmarkDelegationID = "50000000-0000-4000-8000-000000000001"
)

func BenchmarkAuthorizeRRsetBatch(b *testing.B) {
	conn, _, _ := newTestSchema(b)
	if err := Migrate(b.Context(), conn); err != nil {
		b.Fatalf("Migrate() error = %v", err)
	}
	seedAuthorizationBenchmark(b, conn)
	store := NewStore(conn)

	for _, size := range []int{1, MaxRRsetBatch} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			tuples := make([]RRsetTuple, size)
			for i := range tuples {
				tuples[i] = RRsetTuple{
					Owner:      "host-" + strconv.Itoa(i) + ".example.org.",
					RecordType: "A",
					ChangeKind: "REPLACE",
				}
			}
			ctx := b.Context()

			b.ReportAllocs()
			var (
				decision AuthorizationDecision
				err      error
			)
			for b.Loop() {
				decision, err = store.AuthorizeRRsetBatch(
					ctx, authorizationBenchmarkToken, "benchmark-request",
					"default", "example.org.", tuples,
				)
			}
			if err != nil || !decision.Allowed || len(decision.Denied) != 0 ||
				decision.ZoneBindingID != authorizationBenchmarkBindingID ||
				len(decision.MatchedDelegationIDs) != 1 || decision.MatchedDelegationIDs[0] != authorizationBenchmarkDelegationID {
				b.Fatalf("AuthorizeRRsetBatch(%d) = %+v, %v", size, decision, err)
			}
		})
	}
}

func seedAuthorizationBenchmark(b *testing.B, conn DBTX) {
	b.Helper()
	digest := identifier.DigestToken(authorizationBenchmarkToken)
	benchmarkExec(b, conn, `INSERT INTO identities (id, kind, handle) VALUES ($1, 'service', 'benchmark-writer')`, authorizationBenchmarkIdentityID)
	benchmarkExec(b, conn, `INSERT INTO api_tokens (id, identity_id, label, digest) VALUES ($1, $2, 'benchmark', $3)`, authorizationBenchmarkTokenID, authorizationBenchmarkIdentityID, digest[:])
	benchmarkExec(b, conn, `INSERT INTO zone_bindings (id, generation, upstream, powerdns_zone_id, zone_name, created_by_identity_id) VALUES ($1, 1, 'default', 'example.org.', 'example.org.', $2)`, authorizationBenchmarkBindingID, authorizationBenchmarkIdentityID)
	benchmarkExec(b, conn, `INSERT INTO delegations (id, zone_binding_id, grantee_identity_id, created_by_identity_id) VALUES ($1, $2, $3, $3)`, authorizationBenchmarkDelegationID, authorizationBenchmarkBindingID, authorizationBenchmarkIdentityID)
	benchmarkExec(b, conn, `INSERT INTO delegation_selectors (delegation_id, position, kind, selector, sql_like_pattern) VALUES ($1, 1, 'glob', '*.example.org.', '%.example.org.')`, authorizationBenchmarkDelegationID)
}

func benchmarkExec(b *testing.B, conn DBTX, query string, args ...any) {
	b.Helper()
	if _, err := conn.Exec(b.Context(), query, args...); err != nil {
		b.Fatalf("seed authorization benchmark: %v", err)
	}
}
