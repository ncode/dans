//go:build integration

package httpserver

import (
	"context"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ncode/dans/internal/benchtest"
	"github.com/ncode/dans/internal/database"
)

func benchmarkPostgresApplication(b testing.TB, denied bool) (http.Handler, *pgx.Conn, *database.RuntimePools, *atomic.Int64) {
	b.Helper()
	conn, dsn := benchtest.NewPostgres(b)
	if err := database.Migrate(b.Context(), conn); err != nil {
		b.Fatal(err)
	}
	benchtest.Seed(b, conn)
	if denied {
		benchtest.Exec(b, conn, `UPDATE delegation_selectors SET kind='exact', selector='host-0.example.org.', sql_like_pattern='host-0.example.org.'`)
	}
	pools, err := database.OpenRuntimePools(b.Context(), database.RuntimePoolConfig{ConnectionString: dsn, MaxConnections: 16, AuthenticationReserve: 4, ReadinessReserve: 1, ConnectTimeout: 5 * time.Second, StatementTimeout: 5 * time.Second, LockTimeout: time.Second, TransactionTimeout: 5 * time.Second})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(pools.Close)
	for _, pool := range []*database.Store{database.NewStore(pools.Authentication), database.NewStore(pools.Requests)} {
		if _, err := pool.Authenticate(b.Context(), benchtest.Token); err != nil {
			b.Fatal(err)
		}
	}
	client, calls := benchmarkUpstream(b)
	store := database.NewStore(pools.Requests)
	failures := &recordingAuditFailureReporter{}
	b.Cleanup(func() {
		if failures.count() != 0 || failures.lifecycleCount() != 0 {
			b.Error("audit persistence failure")
		}
	})
	operations, err := NewDANSOperations(GeneratedConfig{Store: store, Upstream: client, UpstreamID: "default", MutationTimeout: 5 * time.Second, AuditFailures: failures})
	if err != nil {
		b.Fatal(err)
	}
	handler := benchmarkApplication(b, ApplicationConfig{Upstream: client, Authenticator: database.NewStore(pools.Authentication), Schema: func(ctx context.Context) error { return database.CheckRuntimeCompatibility(ctx, pools.Requests) }, DANS: operations, Mutations: store, Lifecycle: store, Denials: store, AuditFailures: failures, LifecycleFailures: failures})
	return handler, conn, pools, calls
}

func BenchmarkApplicationPostgresRead(b *testing.B) { benchmarkPostgresSerial(b, 0, false) }

func BenchmarkApplicationPostgresWrite(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(strconv.Itoa(size), func(b *testing.B) { benchmarkPostgresSerial(b, size, false) })
	}
	b.Run("Denied", func(b *testing.B) { benchmarkPostgresSerial(b, 2, true) })
}

func benchmarkPostgresSerial(b *testing.B, size int, denied bool) {
	handler, conn, pools, calls := benchmarkPostgresApplication(b, denied)
	method, status := http.MethodGet, http.StatusOK
	var payload []byte
	if size > 0 {
		method, status = http.MethodPatch, http.StatusNoContent
		payload = benchmarkPatch(b, size, 1, 0)
	}
	if denied {
		status = http.StatusForbidden
	}
	request := benchmarkRequest(method, payload)
	request.Header.Set("X-API-Key", benchtest.Token)
	writer := benchmarkCapture{benchmarkResponseWriter: benchmarkResponseWriter{header: make(http.Header)}}
	invoke := func() {
		benchmarkServe(handler, request, payload, &writer)
		if err := benchmarkResponseError(&writer, status); err != nil {
			b.Fatal(err)
		}
	}
	// A read warms the upstream and shared checks without changing the audit seed.
	read := benchmarkRequest(http.MethodGet, nil)
	read.Header.Set("X-API-Key", benchtest.Token)
	benchmarkServe(handler, read, nil, &writer)
	if err := benchmarkResponseError(&writer, http.StatusOK); err != nil {
		b.Fatal(err)
	}
	initial, warm := benchtest.AuditRows(b, conn), calls.Load()
	b.ReportAllocs()
	if payload != nil {
		b.SetBytes(int64(len(payload)))
	}
	for b.Loop() {
		invoke()
	}
	if err := benchmarkResponseError(&writer, status); err != nil {
		b.Fatal(err)
	}
	writes, denials, upstreamCalls := int64(0), int64(0), int64(b.N)
	if size > 0 {
		writes = int64(b.N)
	}
	if denied {
		writes, denials, upstreamCalls = 0, int64(b.N), 0
	}
	checkApplicationAudit(b, conn, initial, writes, denials)
	if calls.Load()-warm != upstreamCalls {
		b.Fatal("incorrect upstream call count")
	}
	checkBenchmarkPools(b, pools)
}

func checkApplicationAudit(b *testing.B, conn *pgx.Conn, initial, writes, denials int64) {
	b.Helper()
	benchtest.CheckAuditRows(b, conn, initial, 2*writes+denials)
	var pairs, denied int64
	if err := conn.QueryRow(b.Context(), `SELECT count(*) FROM audit_events o JOIN audit_events i ON o.intent_event_id=i.id AND o.operation_id=i.operation_id WHERE i.event_kind='dns_intent' AND o.result='succeeded'`).Scan(&pairs); err != nil {
		b.Fatal(err)
	}
	if err := conn.QueryRow(b.Context(), `SELECT count(*) FROM audit_events WHERE event_kind='authorization_denied'`).Scan(&denied); err != nil {
		b.Fatal(err)
	}
	if pairs != writes || denied != denials {
		b.Fatalf("audit pairs/denials = %d/%d, want %d/%d", pairs, denied, writes, denials)
	}
}

func checkBenchmarkPools(b testing.TB, pools *database.RuntimePools) {
	b.Helper()
	if pools.Requests.Stat().AcquiredConns() != 0 || pools.Authentication.Stat().AcquiredConns() != 0 || pools.Readiness.Stat().AcquiredConns() != 0 {
		b.Error("benchmark retained database connections")
	}
}

func BenchmarkApplicationPostgresParallelRead(b *testing.B) { benchmarkPostgresParallel(b, "Read") }
func BenchmarkApplicationPostgresParallelWrite(b *testing.B) {
	for _, mode := range []string{"Write", "Mixed"} {
		b.Run(mode, func(b *testing.B) { benchmarkPostgresParallel(b, mode) })
	}
}

func benchmarkPostgresParallel(b *testing.B, mode string) {
	handler, conn, pools, calls := benchmarkPostgresApplication(b, false)
	payload := benchmarkPatch(b, 1, 1, 0)
	read, write := benchmarkRequest(http.MethodGet, nil), benchmarkRequest(http.MethodPatch, payload)
	read.Header.Set("X-API-Key", benchtest.Token)
	write.Header.Set("X-API-Key", benchtest.Token)
	writer := benchmarkCapture{benchmarkResponseWriter: benchmarkResponseWriter{header: make(http.Header)}}
	benchmarkServe(handler, read, nil, &writer)
	if err := benchmarkResponseError(&writer, http.StatusOK); err != nil {
		b.Fatal(err)
	}
	initial, warm := benchtest.AuditRows(b, conn), calls.Load()
	var reads, writes, workers atomic.Int64
	var failure error
	var once sync.Once
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		workers.Add(1)
		writer := benchmarkCapture{benchmarkResponseWriter: benchmarkResponseWriter{header: make(http.Header)}}
		var localReads, localWrites int64
		for i := 0; pb.Next(); i++ {
			isWrite := mode == "Write" || mode == "Mixed" && i%10 == 9
			request, body, status := read, []byte(nil), http.StatusOK
			if isWrite {
				request, body, status = write, payload, http.StatusNoContent
				localWrites++
			} else {
				localReads++
			}
			benchmarkServe(handler, request, body, &writer)
			if err := benchmarkResponseError(&writer, status); err != nil {
				once.Do(func() { failure = err })
			}
		}
		reads.Add(localReads)
		writes.Add(localWrites)
	})
	b.StopTimer()
	if failure != nil {
		b.Fatal(failure)
	}
	if reads.Load()+writes.Load() != int64(b.N) || calls.Load()-warm != int64(b.N) {
		b.Fatal("concurrent request count mismatch")
	}
	checkApplicationAudit(b, conn, initial, writes.Load(), 0)
	checkBenchmarkPools(b, pools)
	b.ReportMetric(float64(reads.Load()), "reads")
	b.ReportMetric(float64(writes.Load()), "writes")
	b.ReportMetric(float64(workers.Load()), "workers")
	b.ReportMetric(float64(runtime.GOMAXPROCS(0)), "gomaxprocs")
	b.ReportMetric(16, "pool-conns")
	b.ReportMetric(32, "request-limit")
}

func TestBenchmarkApplicationCancellationReleasesWorkers(t *testing.T) {
	handler, _, pools, _ := benchmarkPostgresApplication(t, false)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			request := benchmarkRequest(http.MethodGet, nil).WithContext(ctx)
			request.Header.Set("X-API-Key", benchtest.Token)
			writer := benchmarkCapture{benchmarkResponseWriter: benchmarkResponseWriter{header: make(http.Header)}}
			benchmarkServe(handler, request, nil, &writer)
			if writer.status != http.StatusServiceUnavailable {
				t.Errorf("cancelled status = %d", writer.status)
			}
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled workers did not return")
	}
	checkBenchmarkPools(t, pools)
}
