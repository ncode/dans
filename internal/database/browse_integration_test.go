//go:build integration

package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowseGenerationsFencingAndMutationReplay(t *testing.T) {
	t.Parallel()
	conn, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	other := NewStore(connectToTestSchema(t, dsn, schema))
	actor := insertTestOperator(t, conn, "871")
	first, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false)
	if err != nil || first.State != "indexing" || len(first.Items) != 0 {
		t.Fatalf("initial: %+v %v", first, err)
	}
	claim, err := store.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ClaimBrowseFull(t.Context(), "default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate claim: %v", err)
	}
	sets := make([]json.RawMessage, 201)
	for i := range sets {
		sets[i] = json.RawMessage(fmt.Sprintf(`{"name":"r%03d.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.1","disabled":false},{"content":"192.0.2.2","disabled":false}]}`, i))
	}
	if err := store.StageBrowseRRsets(t.Context(), claim, sets); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishBrowseFull(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	page, err := other.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false)
	if err != nil || len(page.Items) != 100 || page.NextCursor == nil || page.State != "ready" {
		t.Fatalf("page: %+v %v", page, err)
	}
	continuation := *page.NextCursor
	next, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{Cursor: continuation}, false)
	if err != nil || len(next.Items) != 100 || string(next.Items[0]) == string(page.Items[0]) {
		t.Fatalf("next: %+v %v", next, err)
	}
	filtered, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{Name: "r12", Type: "A"}, false)
	if err != nil || len(filtered.Items) != 10 {
		t.Fatalf("prefix: %d %v", len(filtered.Items), err)
	}
	if err := store.StageBrowseRRsets(t.Context(), claim, sets[:1]); !errors.Is(err, ErrBrowseLeaseLost) {
		t.Fatalf("late stage: %v", err)
	}
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, true); err != nil {
		t.Fatal(err)
	}
	refresh, err := other.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := store.CreateDNSIntent(t.Context(), actor, DNSIntentInput{Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: "default:example.test.", RRsets: []RRsetTuple{{Owner: "r000.example.test.", RecordType: "A", ChangeKind: "DELETE"}}, RequestDigest: make([]byte, 32), Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordDNSOutcome(t.Context(), DNSOutcomeInput{IntentEventID: intent.EventID, OperationID: intent.OperationID, Result: "unknown"}); err != nil {
		t.Fatal(err)
	}
	change, err := other.ClaimBrowseChange(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := other.PublishBrowseChange(t.Context(), change, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{Cursor: continuation}, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed generation cursor = %v", err)
	}
	if err := other.StageBrowseRRsets(t.Context(), refresh, sets); err != nil {
		t.Fatal(err)
	}
	if err := other.PublishBrowseFull(t.Context(), refresh); err != nil {
		t.Fatal(err)
	}
	replay, err := store.ClaimBrowseChange(t.Context(), "default")
	if err != nil {
		t.Fatalf("full publication lost pending change: %v", err)
	}
	if err := store.PublishBrowseChange(t.Context(), replay, nil); err != nil {
		t.Fatal(err)
	}
	deleted, err := other.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{Name: "r000.example.test.", Match: "exact"}, false)
	if err != nil || len(deleted.Items) != 0 {
		t.Fatalf("delete reconcile: %+v %v", deleted, err)
	}
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, true); err != nil {
		t.Fatal(err)
	}
	interrupted, err := store.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), "UPDATE browse_zones SET full_lease_until=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	recovered, err := other.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishBrowseFull(t.Context(), interrupted); !errors.Is(err, ErrBrowseLeaseLost) {
		t.Fatalf("expired publication: %v", err)
	}
	if err := other.FailBrowseFull(t.Context(), recovered); err != nil {
		t.Fatal(err)
	}
	stale, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false)
	if err != nil || stale.State != "stale" || stale.Error == nil || len(stale.Items) != 100 {
		t.Fatalf("stale: %+v %v", stale, err)
	}
}

func TestBrowseLateOutcomeRetainsReadReconciliation(t *testing.T) {
	t.Parallel()
	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "872")
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false); err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishBrowseFull(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	intent, err := store.CreateDNSIntent(t.Context(), actor, DNSIntentInput{Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: "default:example.test.", RRsets: []RRsetTuple{{Owner: "www.example.test.", RecordType: "TXT", ChangeKind: "EXTEND"}}, RequestDigest: make([]byte, 32), Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), "UPDATE browse_changes SET ready_at=now()"); err != nil {
		t.Fatal(err)
	}
	change, err := store.ClaimBrowseChange(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishBrowseChange(t.Context(), change, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordDNSOutcome(t.Context(), DNSOutcomeInput{IntentEventID: intent.EventID, OperationID: intent.OperationID, Result: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimBrowseChange(t.Context(), "default"); err != nil {
		t.Fatalf("late outcome lost reconciliation: %v", err)
	}
}

func TestBrowseFullDoesNotReconcileUnfinishedIntentEarly(t *testing.T) {
	t.Parallel()
	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "873")
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false); err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateDNSIntent(t.Context(), actor, DNSIntentInput{Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: "default:example.test.", RRsets: []RRsetTuple{{Owner: "www.example.test.", RecordType: "TXT", ChangeKind: "PRUNE"}}, RequestDigest: make([]byte, 32), Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishBrowseFull(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimBrowseChange(t.Context(), "default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unfinished mutation eligible before outcome/deadline: %v", err)
	}
}

func TestBrowseWorkerAPIIngestionAndCrossInstanceFreshness(t *testing.T) {
	testBrowseWorker(t, 201, 1)
}

func TestBrowseWorkerCapacity(t *testing.T) {
	if os.Getenv("DANS_BROWSE_CAPACITY") != "1" {
		t.Skip("set DANS_BROWSE_CAPACITY=1 for 400-zone / 250,000-RRset API ingestion")
	}
	testBrowseWorker(t, 250000, 400)
}

func testBrowseWorker(t *testing.T, count, zones int) {
	t.Helper()
	conn, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	actor := insertTestOperator(t, conn, "874")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg2, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg2.ConnConfig.RuntimeParams["search_path"] = schema
	pool2, err := pgxpool.NewWithConfig(t.Context(), cfg2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool2.Close)
	store, other := NewStore(pool), NewStore(pool2)
	var updated atomic.Bool
	var fullReads, exactReads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("worker issued DNS mutation: %s", r.Method)
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		name := r.URL.Query().Get("rrset_name")
		if name != "" {
			exactReads.Add(1)
		} else {
			fullReads.Add(1)
		}
		_, _ = io.WriteString(w, `{"id":"example.test.","rrsets":[`)
		rows := count
		if !strings.HasSuffix(r.URL.Path, "/example.test.") {
			rows = 1
		}
		if name != "" {
			rows = 1
		}
		encoder := json.NewEncoder(w)
		for i := 0; i < rows; i++ {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			owner := fmt.Sprintf("r%06d.example.test.", i)
			if name != "" {
				owner = name
			}
			content := "192.0.2.1"
			if updated.Load() {
				content = "192.0.2.99"
			}
			if err := encoder.Encode(api.RRSet{Name: owner, Type: "A", Ttl: 60, Records: []api.Record{{Content: content}, {Content: "192.0.2.2", Disabled: new(true)}}}); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, "]}")
	}))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: 5 * time.Minute}, httpapi.NewSecret("synthetic-index-key"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < zones; i++ {
		if _, err := store.BrowseRRsets(t.Context(), actor, "default", fmt.Sprintf("zone%03d.test.", i), BrowseOptions{}, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		defer close(done)
		done <- NewBrowseWorker(other, client, "default", func() bool { return true }).Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("worker shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("worker did not stop")
		}
	})
	await := func(check func() bool, timeout time.Duration) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			select {
			case err := <-done:
				t.Fatalf("worker exited: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
		}
		t.Fatal("timed out waiting for indexed state")
	}
	await(func() bool {
		page, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false)
		return err == nil && page.State == "ready"
	}, 4*time.Minute)
	cold := time.Since(start)
	var stored int
	if err := conn.QueryRow(t.Context(), "SELECT count(*) FROM browse_rrsets r JOIN browse_zones z ON z.id=r.zone WHERE z.zone_id='example.test.' AND r.generation=z.generation").Scan(&stored); err != nil || stored != count {
		t.Fatalf("ingested %d/%d: %v", stored, count, err)
	}
	pageStart := time.Now()
	page, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{Name: "r000", Type: "A"}, false)
	if err != nil || len(page.Items) != min(100, count) || page.NextCursor == nil {
		t.Fatalf("warm page %d %v", len(page.Items), err)
	}
	warm := time.Since(pageStart)
	fullBefore := fullReads.Load()
	intent, err := store.CreateDNSIntent(t.Context(), actor, DNSIntentInput{Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: "default:example.test.", RRsets: []RRsetTuple{{Owner: "r000000.example.test.", RecordType: "A", ChangeKind: "REPLACE"}}, RequestDigest: make([]byte, 32), Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	updated.Store(true)
	if _, err := store.RecordDNSOutcome(t.Context(), DNSOutcomeInput{IntentEventID: intent.EventID, OperationID: intent.OperationID, Result: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	changed := time.Now()
	await(func() bool {
		page, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{Name: "r000000.example.test.", Match: "exact"}, false)
		return err == nil && len(page.Items) == 1 && strings.Contains(string(page.Items[0]), "192.0.2.99")
	}, 5*time.Second)
	lag := time.Since(changed)
	if exactReads.Load() == 0 {
		t.Fatal("mutation did not use an exact API read")
	}
	if zones == 1 && fullReads.Load() != fullBefore {
		t.Fatal("ordinary RRset mutation rebuilt entire zone")
	}
	if zones > 1 {
		await(func() bool {
			var ready int
			err := conn.QueryRow(t.Context(), "SELECT count(*) FROM browse_zones WHERE generation IS NOT NULL").Scan(&ready)
			return err == nil && ready == zones
		}, 4*time.Minute)
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	t.Logf("synthetic API ingestion: zones=%d largest_rrsets=%d cold=%s warm_filtered_page=%s cross_instance_lag=%s total=%s heap_bytes=%d total_alloc_bytes=%d full_reads=%d exact_reads=%d", zones, count, cold, warm, lag, time.Since(start), memory.HeapAlloc, memory.TotalAlloc, fullReads.Load(), exactReads.Load())
}

func TestBrowseDeletionOutcomeFencesInterveningRead(t *testing.T) {
	t.Parallel()
	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "875")
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false); err != nil {
		t.Fatal(err)
	}
	old, err := store.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PrepareZoneDeletionByUpstreamZoneID(t.Context(), actor, ZoneDeletionByUpstreamInput{Upstream: "default", PowerDNSZoneID: "example.test.", RequestDigest: make([]byte, 32), Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishBrowseFull(t.Context(), old); !errors.Is(err, ErrBrowseLeaseLost) {
		t.Fatalf("pre-deletion worker was not fenced: %v", err)
	}
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false); err != nil {
		t.Fatal(err)
	}
	between, err := store.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordZoneDeletionOutcome(t.Context(), plan, ZoneDeletionOutcomeInput{Result: ZoneDeletionUnknown, ResponseClass: "transport_error"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishBrowseFull(t.Context(), between); !errors.Is(err, ErrBrowseLeaseLost) {
		t.Fatalf("read started during deletion published after outcome: %v", err)
	}
}

func TestBrowseSameRRsetReadsCannotPublishOutOfOrder(t *testing.T) {
	t.Parallel()
	conn, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	other := NewStore(connectToTestSchema(t, dsn, schema))
	actor := insertTestOperator(t, conn, "876")
	if _, err := store.BrowseRRsets(t.Context(), actor, "default", "example.test.", BrowseOptions{}, false); err != nil {
		t.Fatal(err)
	}
	full, err := store.ClaimBrowseFull(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishBrowseFull(t.Context(), full); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		intent, err := store.CreateDNSIntent(t.Context(), actor, DNSIntentInput{Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: "default:example.test.", RRsets: []RRsetTuple{{Owner: "www.example.test.", RecordType: "A", ChangeKind: "REPLACE"}}, RequestDigest: make([]byte, 32), Deadline: time.Now().Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.RecordDNSOutcome(t.Context(), DNSOutcomeInput{IntentEventID: intent.EventID, OperationID: intent.OperationID, Result: "succeeded"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ClaimBrowseChange(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ClaimBrowseChange(t.Context(), "default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another instance acquired same RRset while read pending: %v", err)
	}
	if err := store.PublishBrowseChange(t.Context(), first, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := other.ClaimBrowseChange(t.Context(), "default"); err != nil {
		t.Fatalf("remaining read lost: %v", err)
	}
}
