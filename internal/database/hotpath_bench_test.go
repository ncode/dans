//go:build integration

package database

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ncode/dans/internal/benchtest"
)

func newDatabaseBenchmark(b *testing.B) (*pgx.Conn, *Store) {
	b.Helper()
	conn, _ := benchtest.NewPostgres(b)
	if err := Migrate(b.Context(), conn); err != nil {
		b.Fatal(err)
	}
	benchtest.Seed(b, conn)
	store := NewStore(conn)
	if _, err := store.Authenticate(b.Context(), benchtest.Token); err != nil {
		b.Fatal(err)
	}
	return conn, store
}

func benchmarkTuples(size int) []RRsetTuple {
	tuples := make([]RRsetTuple, size)
	for i := range tuples {
		tuples[i] = RRsetTuple{Owner: fmt.Sprintf("host-%d.example.org.", i), RecordType: "A", ChangeKind: "REPLACE"}
	}
	return tuples
}

func BenchmarkAuthenticatePostgres(b *testing.B) {
	for _, name := range []string{"Valid", "Unknown", "Revoked", "Expired"} {
		b.Run(name, func(b *testing.B) {
			conn, store := newDatabaseBenchmark(b)
			token := benchtest.Token
			switch name {
			case "Unknown":
				token = token[:len(token)-1] + "Q"
			case "Revoked":
				benchtest.Exec(b, conn, "UPDATE api_tokens SET revoked_at = now()")
			case "Expired":
				benchtest.Exec(b, conn, "UPDATE api_tokens SET created_at = now()-interval '2 hours', expires_at = now()-interval '1 hour'")
			}
			b.ReportAllocs()
			var actor Actor
			for b.Loop() {
				var err error
				actor, err = store.Authenticate(b.Context(), token)
				if name == "Valid" && err != nil || name != "Valid" && !errors.Is(err, ErrUnauthenticated) {
					b.Fatalf("authentication result: %v", err)
				}
			}
			if name == "Valid" && (actor.IdentityID != benchtest.IdentityID || actor.TokenID != benchtest.TokenID || actor.Kind != "service" || actor.Operator) {
				b.Fatal("incorrect authenticated actor")
			}
			if name != "Valid" && actor != (Actor{}) {
				b.Fatal("rejected credential returned an actor")
			}
		})
	}
}

func BenchmarkRuntimeCompatibilityPostgres(b *testing.B) {
	conn, _ := newDatabaseBenchmark(b)
	if err := CheckRuntimeCompatibility(b.Context(), conn); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := CheckRuntimeCompatibility(b.Context(), conn); err != nil {
			b.Fatal(err)
		}
	}
	if err := CheckRuntimeCompatibility(b.Context(), conn); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkAuthorizationGrants(b *testing.B) {
	for _, test := range []struct {
		name                  string
		population            int
		group, exact, overlap bool
	}{
		{name: "Direct1", population: 1}, {name: "Direct100", population: 100}, {name: "Direct1000", population: 1000},
		{name: "Group", population: 1, group: true}, {name: "Exact", population: 1, exact: true}, {name: "Overlap", population: 100, overlap: true},
	} {
		b.Run(test.name, func(b *testing.B) {
			conn, store := newDatabaseBenchmark(b)
			want := []string{benchtest.DelegationID}
			for i := 2; i <= test.population; i++ {
				id := fmt.Sprintf("50000000-0000-4000-8000-%012d", i)
				benchtest.Exec(b, conn, `INSERT INTO delegations (id, zone_binding_id, grantee_identity_id, created_by_identity_id) VALUES ($1,$2,$3,$3)`, id, benchtest.BindingID, benchtest.IdentityID)
				pattern := fmt.Sprintf("absent-%d.example.org.", i)
				selector := pattern
				if test.overlap {
					pattern = "%.example.org."
					selector = "*.example.org."
					want = append(want, id)
				}
				benchtest.Exec(b, conn, `INSERT INTO delegation_selectors (delegation_id, position, kind, selector, sql_like_pattern) VALUES ($1,1,'glob',$2,$3)`, id, selector, pattern)
			}
			if test.group {
				benchtest.Exec(b, conn, `INSERT INTO groups (id,handle) VALUES ('20000000-0000-4000-8000-000000000001','benchmark-group')`)
				benchtest.Exec(b, conn, `INSERT INTO group_memberships (group_id,identity_id,added_by_identity_id) VALUES ('20000000-0000-4000-8000-000000000001',$1,$1)`, benchtest.IdentityID)
				benchtest.Exec(b, conn, `UPDATE delegations SET grantee_identity_id=NULL, grantee_group_id='20000000-0000-4000-8000-000000000001'`)
			}
			tuples := benchmarkTuples(100)
			if test.exact {
				benchtest.Exec(b, conn, `DELETE FROM delegation_selectors`)
				for i, tuple := range tuples {
					benchtest.Exec(b, conn, `INSERT INTO delegation_selectors (delegation_id,position,kind,selector,sql_like_pattern) VALUES ($1,$2,'exact',$3,$3)`, benchtest.DelegationID, i+1, tuple.Owner)
				}
			}
			for _, table := range []string{"identities", "api_tokens", "zone_bindings", "delegations", "delegation_selectors", "groups", "group_memberships"} {
				benchtest.Exec(b, conn, "ANALYZE "+table)
			}
			invoke := func() AuthorizationDecision {
				decision, err := store.AuthorizeRRsetBatch(b.Context(), benchtest.Token, "benchmark", "default", "example.org.", tuples)
				if err != nil || !decision.Allowed || len(decision.Denied) != 0 || !slices.Equal(decision.MatchedDelegationIDs, want) {
					b.Fatalf("grant decision mismatch: %v", err)
				}
				return decision
			}
			invoke()
			b.ReportAllocs()
			var decision AuthorizationDecision
			for b.Loop() {
				decision = invoke()
			}
			if decision.ZoneBindingID != benchtest.BindingID {
				b.Fatal("incorrect binding")
			}
			benchtest.CheckAuditRows(b, conn, 0, 0)
		})
	}
}

// Fixed-work evidence: use an explicit -benchtime=Nx and a fresh leaf fixture.
func BenchmarkAuthorizationDeniedWrite(b *testing.B) {
	for _, size := range []int{1, 100} {
		for _, partial := range []bool{false, true} {
			if size == 1 && partial {
				continue
			}
			b.Run(strconv.Itoa(size)+"/Partial="+strconv.FormatBool(partial), func(b *testing.B) {
				conn, store := newDatabaseBenchmark(b)
				tuples := benchmarkTuples(size)
				start := 0
				if partial {
					start = size - 1
				}
				for i := start; i < size; i++ {
					tuples[i].Owner = "outside.example.net."
				}
				initial := benchtest.AuditRows(b, conn)
				b.ReportAllocs()
				var decision AuthorizationDecision
				for b.Loop() {
					var err error
					decision, err = store.AuthorizeRRsetBatch(b.Context(), benchtest.Token, "benchmark", "default", "example.org.", tuples)
					if err != nil || decision.Allowed || len(decision.Denied) != size-start || len(decision.MatchedDelegationIDs) != 0 {
						b.Fatalf("denial mismatch: %v", err)
					}
					for i, denied := range decision.Denied {
						if denied.Index != start+i || denied.Owner != tuples[start+i].Owner || denied.RecordType != "A" || denied.ChangeKind != "REPLACE" {
							b.Fatal("incorrect denied tuple")
						}
					}
				}
				if decision.Actor.IdentityID != benchtest.IdentityID {
					b.Fatal("incorrect denial actor")
				}
				benchtest.CheckAuditRows(b, conn, initial, int64(b.N))
				var denials int64
				if err := conn.QueryRow(b.Context(), "SELECT count(*) FROM audit_events WHERE event_kind='authorization_denied'").Scan(&denials); err != nil || denials != int64(b.N) {
					b.Fatalf("denial events = %d: %v", denials, err)
				}
			})
		}
	}
}

func BenchmarkDNSAuditWrite(b *testing.B) {
	for _, size := range []int{1, 100} {
		for _, result := range []string{"succeeded", "failed"} {
			b.Run(strconv.Itoa(size)+"/"+result, func(b *testing.B) {
				conn, store := newDatabaseBenchmark(b)
				actor, err := store.Authenticate(b.Context(), benchtest.Token)
				if err != nil {
					b.Fatal(err)
				}
				actor.RequestID = "benchmark"
				input := DNSIntentInput{Action: "powerdns.zone.patch", TargetKind: "zone_binding", TargetID: benchtest.BindingID, RRsets: benchmarkTuples(size), MatchedDelegationIDs: []string{benchtest.DelegationID}, RequestDigest: make([]byte, 32), Deadline: time.Now().Add(time.Hour)}
				status, class := 204, "2xx"
				if result == "failed" {
					status, class = 422, "4xx"
				}
				initial := benchtest.AuditRows(b, conn)
				b.ReportAllocs()
				var intent DNSIntent
				var outcome DNSOutcome
				for b.Loop() {
					intent, err = store.CreateDNSIntent(b.Context(), actor, input)
					if err != nil {
						b.Fatal(err)
					}
					outcome, err = store.RecordDNSOutcome(b.Context(), DNSOutcomeInput{IntentEventID: intent.EventID, OperationID: intent.OperationID, Result: result, ResponseClass: class, ResponseCode: &status})
					if err != nil {
						b.Fatal(err)
					}
				}
				if outcome.IntentEventID != intent.EventID || outcome.OperationID != intent.OperationID || outcome.Result != result {
					b.Fatal("audit pair mismatch")
				}
				benchtest.CheckAuditRows(b, conn, initial, int64(2*b.N))
				var paired int64
				if err := conn.QueryRow(b.Context(), `SELECT count(*) FROM audit_events o JOIN audit_events i ON o.intent_event_id=i.id AND o.operation_id=i.operation_id WHERE i.event_kind='dns_intent' AND o.result=$1`, result).Scan(&paired); err != nil || paired != int64(b.N) {
					b.Fatalf("audit pairs = %d: %v", paired, err)
				}
			})
		}
	}
}
