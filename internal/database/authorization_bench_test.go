//go:build integration

package database

import (
	"strconv"
	"testing"

	"github.com/ncode/dans/internal/benchtest"
)

func BenchmarkAuthorizeRRsetBatch(b *testing.B) {
	for _, size := range []int{1, MaxRRsetBatch} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			_, store := newDatabaseBenchmark(b)
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
					ctx, benchtest.Token, "benchmark-request",
					"default", "example.org.", tuples,
				)
				if err != nil || !decision.Allowed {
					b.Fatalf("authorization failed: %v", err)
				}
			}
			if err != nil || !decision.Allowed || len(decision.Denied) != 0 ||
				decision.ZoneBindingID != benchtest.BindingID ||
				len(decision.MatchedDelegationIDs) != 1 || decision.MatchedDelegationIDs[0] != benchtest.DelegationID {
				b.Fatalf("AuthorizeRRsetBatch(%d) = %+v, %v", size, decision, err)
			}
		})
	}
}
