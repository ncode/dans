package httpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

// BenchmarkZoneDeletion measures handler orchestration and real loopback HTTP.
// Database preparation and audit writes have constant cost; database latency,
// authentication middleware, and incoming request parsing are excluded.
func BenchmarkZoneDeletion(b *testing.B) {
	for _, retry := range []bool{false, true} {
		path := "Initial"
		if retry {
			path = "Retry"
		}
		for _, scenario := range []struct {
			name   string
			status int
			bytes  int
			delay  time.Duration
		}{
			{name: "NoContent", status: http.StatusNoContent},
			{name: "NotFound4KiB", status: http.StatusNotFound, bytes: 4 << 10},
			{name: "NotFound64KiB", status: http.StatusNotFound, bytes: 64 << 10},
			{name: "NotFound4KiBDelayed10ms", status: http.StatusNotFound, bytes: 4 << 10, delay: 10 * time.Millisecond},
		} {
			b.Run(path+"/"+scenario.name, func(b *testing.B) {
				body := []byte(strings.Repeat("x", scenario.bytes))
				var connections atomic.Int64
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)))
					w.WriteHeader(scenario.status)
					if scenario.delay != 0 {
						w.(http.Flusher).Flush()
						timer := time.NewTimer(scenario.delay)
						defer timer.Stop()
						select {
						case <-timer.C:
						case <-request.Context().Done():
							return
						}
					}
					_, _ = w.Write(body) // Retry may close the response before its body arrives.
				}))
				server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						connections.Add(1)
					}
				}
				server.Start()
				b.Cleanup(server.Close)
				client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
				if err != nil {
					b.Fatal(err)
				}
				plan := database.ZoneDeletionPlan{
					Binding: testZoneBinding(time.Now(), true),
					Intent: database.DNSIntent{
						EventID: "00000000-0000-4000-8000-000000000070", OperationID: "00000000-0000-4000-8000-000000000071",
					},
					Upstream: "primary", PowerDNSZoneID: "existing.example.", ZoneName: "existing.example.",
				}
				wantResult := database.ZoneDeletionDeleted
				if scenario.status == http.StatusNotFound {
					wantResult = database.ZoneDeletionNotFound
				}
				var started time.Time
				var auditNanos int64
				var auditCalls int
				store := &benchmarkZoneDeletionStore{
					plan: plan,
					zoneStoreStub: &zoneStoreStub{
						retry: func(_ context.Context, _ database.Actor, input database.ZoneDeletionInput) (database.ZoneDeletionPlan, error) {
							prepared := plan
							prepared.Intent.Deadline = input.Deadline
							return prepared, nil
						},
						deletionOutcome: func(_ context.Context, _ database.ZoneDeletionPlan, input database.ZoneDeletionOutcomeInput) (database.DNSOutcome, error) {
							auditNanos += time.Since(started).Nanoseconds()
							auditCalls++
							if input.Result != wantResult {
								b.Fatalf("outcome = %s, want %s", input.Result, wantResult)
							}
							return database.DNSOutcome{}, nil
						},
					},
				}
				failures := &recordingAuditFailureReporter{}
				handler, err := NewZoneHandler(store, client, "primary", time.Second, failures, &strictFallback{})
				if err != nil {
					b.Fatal(err)
				}
				proxy := &PowerDNSProxy{Upstream: client, Lifecycle: store, UpstreamID: "primary", MutationTimeout: time.Second, AuditFailures: failures}
				request := operatorRequestContext(httptest.NewRequest(http.MethodDelete, "/api/v1/servers/localhost/zones/existing.example.", nil), testRequestID)
				retryRequest := api.RetryZoneBindingDeletionRequestObject{ZoneBindingId: uuid.MustParse(testBindingID)}
				writer := benchmarkResponseWriter{header: make(http.Header)}
				invoke := func() {
					started = time.Now()
					if retry {
						response, err := handler.RetryZoneBindingDeletion(request.Context(), retryRequest)
						if _, ok := response.(api.RetryZoneBindingDeletion204Response); err != nil || !ok {
							b.Fatalf("retry response = %T, error = %v", response, err)
						}
					} else {
						writer.status = 0
						clear(writer.header)
						proxy.DeleteZone(&writer, request, api.ServerId("localhost"), api.ZoneId("existing.example."))
						if writer.status != scenario.status {
							b.Fatalf("status = %d, want %d", writer.status, scenario.status)
						}
					}
				}
				invoke() // Warm the connection where the response permits reuse.
				auditNanos, auditCalls = 0, 0
				warmConnections := connections.Load()
				b.ReportAllocs()
				for b.Loop() {
					invoke()
				}
				if auditCalls != b.N {
					b.Fatalf("audit writes = %d, want %d", auditCalls, b.N)
				}
				b.ReportMetric(float64(auditNanos)/float64(b.N), "audit-ns/op")
				b.ReportMetric(float64(connections.Load()-warmConnections)/float64(b.N), "conns/op")
			})
		}
	}
}

type benchmarkZoneDeletionStore struct {
	*zoneStoreStub
	plan database.ZoneDeletionPlan
}

func (store *benchmarkZoneDeletionStore) PrepareZoneDeletionByUpstreamZoneID(_ context.Context, _ database.Actor, input database.ZoneDeletionByUpstreamInput) (database.ZoneDeletionPlan, error) {
	plan := store.plan
	plan.Intent.Deadline = input.Deadline
	return plan, nil
}

func (*benchmarkZoneDeletionStore) BindCreatedZone(context.Context, database.Actor, database.ZoneBindingInput) (database.ZoneBinding, error) {
	return database.ZoneBinding{}, database.ErrInvalid
}
