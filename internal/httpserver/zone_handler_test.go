package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

func TestZoneHandlerLazilyBindsObservedZone(t *testing.T) {
	t.Parallel()

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.EscapedPath() != "/api/v1/servers/localhost/zones/existing.example." || request.URL.Query().Get("rrsets") != "false" {
			t.Errorf("upstream request = %s %s", request.Method, request.URL.String())
		}
		_, _ = io.WriteString(w, `{"id":"existing.example.","name":"existing.example.","upstream_extension":true}`)
	}))
	t.Cleanup(upstreamServer.Close)
	createdAt := time.Date(2026, time.August, 14, 17, 0, 0, 0, time.UTC)
	store := &zoneStoreStub{ensure: func(_ context.Context, actor database.Actor, input database.ZoneBindingInput) (database.ZoneBinding, error) {
		if actor.RequestID != testRequestID || input.Upstream != "primary" || input.PowerDNSZoneID != "existing.example." || input.ZoneName != "existing.example." {
			t.Errorf("actor/input = %+v %+v", actor, input)
		}
		return testZoneBinding(createdAt, false), nil
	}}
	handler := newTestZoneServer(t, store, upstreamServer.URL)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, actorRequest(http.MethodPost, "/api/v1/dans/zone-bindings", []byte(`{"zone_id":"existing.example."}`)))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/api/v1/dans/zone-bindings/"+testBindingID {
		t.Errorf("Location = %q", response.Header().Get("Location"))
	}
}

func TestZoneHandlerObservesAbsenceAsRequiredNullWithoutChangingLifecycle(t *testing.T) {
	t.Parallel()

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	t.Cleanup(upstreamServer.Close)
	now := time.Date(2026, time.August, 14, 17, 30, 0, 0, time.UTC)
	store := &zoneStoreStub{
		get: func(context.Context, database.Actor, string) (database.ZoneBinding, error) {
			return testZoneBinding(now, true), nil
		},
		observe: func(_ context.Context, _ database.Actor, input database.ZoneObservationInput) (database.ZoneObservation, error) {
			if input.BindingID != testBindingID || input.ZonePresent || input.ObservedZoneID != nil {
				t.Errorf("observation input = %+v", input)
			}
			return database.ZoneObservation{BindingID: testBindingID, ObservedAt: now, ZonePresent: false}, nil
		},
	}
	handler := newTestZoneServer(t, store, upstreamServer.URL)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, actorRequest(http.MethodPost, "/api/v1/dans/zone-bindings/"+testBindingID+"/observe", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if value, present := body["observed_zone_id"]; !present || value != nil {
		t.Errorf("observed_zone_id = %#v, present=%t", value, present)
	}
}

func TestZoneHandlerRetriesDeletionOnlyAfterDurablePreparation(t *testing.T) {
	t.Parallel()

	store := &zoneStoreStub{}
	var upstreamCalls int
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		store.mu.Lock()
		prepared := store.prepared
		store.mu.Unlock()
		if !prepared {
			t.Error("retry reached upstream before a new durable intent")
		}
		upstreamCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstreamServer.Close)
	store.get = func(context.Context, database.Actor, string) (database.ZoneBinding, error) {
		return testZoneBinding(time.Now().UTC(), true), nil
	}
	store.retry = func(_ context.Context, _ database.Actor, input database.ZoneDeletionInput) (database.ZoneDeletionPlan, error) {
		store.mu.Lock()
		store.prepared = true
		store.mu.Unlock()
		return database.ZoneDeletionPlan{
			Binding:  testZoneBinding(time.Now().UTC(), true),
			Intent:   database.DNSIntent{EventID: "00000000-0000-4000-8000-000000000070", OperationID: "00000000-0000-4000-8000-000000000071", Deadline: input.Deadline},
			Upstream: "primary", PowerDNSZoneID: "existing.example.", ZoneName: "existing.example.",
		}, nil
	}
	auditFailures := &recordingAuditFailureReporter{}
	request := actorRequest(http.MethodPost, "/api/v1/dans/zone-bindings/"+testBindingID+"/retry-delete", nil)
	requestCtx, cancelRequest := context.WithCancel(request.Context())
	defer cancelRequest()
	store.deletionOutcome = func(ctx context.Context, _ database.ZoneDeletionPlan, input database.ZoneDeletionOutcomeInput) (database.DNSOutcome, error) {
		cancelRequest()
		if ctx.Err() != nil {
			t.Error("request cancellation canceled outcome persistence")
		}
		if _, bounded := ctx.Deadline(); !bounded {
			t.Error("outcome persistence has no deadline")
		}
		if input.Result != database.ZoneDeletionDeleted || input.ResponseCode == nil || *input.ResponseCode != http.StatusNoContent {
			t.Errorf("deletion outcome = %+v", input)
		}
		return database.DNSOutcome{}, database.ErrAuditUnavailable
	}
	handler := newTestZoneServerWithAudit(t, store, upstreamServer.URL, auditFailures)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request.WithContext(requestCtx))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if upstreamCalls != 1 {
		t.Errorf("upstream calls = %d, want 1", upstreamCalls)
	}
	if auditFailures.count() != 1 {
		t.Errorf("audit failure reports = %d, want 1", auditFailures.count())
	}
}

func TestZoneDeletionRecordsStatusBeforeBrokenBody(t *testing.T) {
	t.Parallel()

	for _, retry := range []bool{false, true} {
		for _, tt := range []struct {
			status      int
			result      database.ZoneDeletionResult
			class       string
			retryStatus int
		}{
			{0, database.ZoneDeletionUnknown, "transport_error", http.StatusBadGateway},
			{http.StatusOK, database.ZoneDeletionDeleted, "success", http.StatusNoContent},
			{http.StatusNoContent, database.ZoneDeletionDeleted, "success", http.StatusNoContent},
			{http.StatusNotFound, database.ZoneDeletionNotFound, "client_error", http.StatusNoContent},
			{http.StatusInternalServerError, database.ZoneDeletionFailed, "server_error", http.StatusBadGateway},
		} {
			t.Run("retry="+strconv.FormatBool(retry)+"/status="+strconv.Itoa(tt.status), func(t *testing.T) {
				t.Parallel()
				outcomeRecorded := make(chan struct{})
				var outcomeBeforeBody atomic.Bool
				var upstreamCalls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					upstreamCalls.Add(1)
					if tt.status == 0 {
						panic(http.ErrAbortHandler) // Close the connection without a response.
					}
					w.Header().Set("Content-Length", "2")
					w.WriteHeader(tt.status)
					w.(http.Flusher).Flush()
					select {
					case <-outcomeRecorded:
						outcomeBeforeBody.Store(true)
					case <-time.After(250 * time.Millisecond):
					}
					_, _ = io.WriteString(w, "x") // Deliberately truncate the declared body.
				}))
				t.Cleanup(server.Close)
				lifecycle := &recordingZoneLifecycle{outcomeRecorded: outcomeRecorded}
				response := httptest.NewRecorder()
				wantStatus := tt.status
				if tt.status == 0 {
					wantStatus = http.StatusBadGateway
				}
				if retry {
					store := &zoneStoreStub{
						retry: func(ctx context.Context, actor database.Actor, input database.ZoneDeletionInput) (database.ZoneDeletionPlan, error) {
							plan, err := lifecycle.PrepareZoneDeletionByUpstreamZoneID(ctx, actor, database.ZoneDeletionByUpstreamInput{
								Upstream: "primary", PowerDNSZoneID: "existing.example.", RequestDigest: input.RequestDigest, Deadline: input.Deadline,
							})
							plan.Binding = testZoneBinding(time.Now(), true)
							return plan, err
						},
						deletionOutcome: lifecycle.RecordZoneDeletionOutcome,
					}
					handler := newTestZoneServer(t, store, server.URL)
					handler.ServeHTTP(response, actorRequest(http.MethodPost, "/api/v1/dans/zone-bindings/"+testBindingID+"/retry-delete", nil))
					wantStatus = tt.retryStatus
				} else {
					client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
					if err != nil {
						t.Fatal(err)
					}
					proxy := &PowerDNSProxy{Upstream: client, Lifecycle: lifecycle, UpstreamID: "primary", MutationTimeout: time.Second}
					request := operatorRequestContext(httptest.NewRequest(http.MethodDelete, "/api/v1/servers/localhost/zones/existing.example.", nil), testRequestID)
					proxy.DeleteZone(response, request, api.ServerId("localhost"), api.ZoneId("existing.example."))
				}
				server.Close() // Wait for the body-order observation before inspecting it.
				if tt.status != 0 && !outcomeBeforeBody.Load() {
					t.Error("deletion outcome was not recorded before the response body")
				}
				if response.Code != wantStatus {
					t.Errorf("status = %d, want %d", response.Code, wantStatus)
				}
				_, _, outcomes := lifecycle.snapshot()
				if len(outcomes) != 1 {
					t.Fatalf("outcomes = %d, want 1", len(outcomes))
				}
				outcome := outcomes[0]
				if outcome.Result != tt.result || outcome.ResponseClass != tt.class {
					t.Errorf("outcome = %+v, want result %s and class %s", outcome, tt.result, tt.class)
				}
				if tt.status == 0 {
					if outcome.ResponseCode != nil {
						t.Error("transport failure recorded a response status")
					}
				} else if outcome.ResponseCode == nil || *outcome.ResponseCode != tt.status {
					t.Errorf("response code = %v, want %d", outcome.ResponseCode, tt.status)
				}
				if len(outcome.ResponseDigest) != 0 {
					t.Error("deletion outcome retained a response-body digest")
				}
				if upstreamCalls.Load() != 1 {
					t.Errorf("upstream calls = %d, want 1", upstreamCalls.Load())
				}
			})
		}
	}
}

func TestZoneHandlerOwnsEveryBindingAndReconciliationRoute(t *testing.T) {
	t.Parallel()

	upstreamServer := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upstreamServer.Close)
	handler := newTestZoneServer(t, &zoneStoreStub{}, upstreamServer.URL)
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/dans/zone-bindings", ""},
		{http.MethodPost, "/api/v1/dans/zone-bindings", `{"zone_id":"existing.example."}`},
		{http.MethodGet, "/api/v1/dans/zone-bindings/" + testBindingID, ""},
		{http.MethodPost, "/api/v1/dans/zone-bindings/" + testBindingID + "/observe", ""},
		{http.MethodPost, "/api/v1/dans/zone-bindings/" + testBindingID + "/confirm-absent", ""},
		{http.MethodPost, "/api/v1/dans/zone-bindings/" + testBindingID + "/retry-delete", ""},
		{http.MethodPost, "/api/v1/dans/zone-bindings/" + testBindingID + "/rebind", `{"zone_id":"existing.example."}`},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, actorRequest(tt.method, tt.path, []byte(tt.body)))
			if response.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
			}
		})
	}
}

func newTestZoneServer(t *testing.T, store ZoneManagementStore, endpoint string) http.Handler {
	return newTestZoneServerWithAudit(t, store, endpoint, &recordingAuditFailureReporter{})
}

func newTestZoneServerWithAudit(t *testing.T, store ZoneManagementStore, endpoint string, auditFailures AuditFailureReporter) http.Handler {
	t.Helper()
	client, err := upstream.New(upstream.TransportConfig{URL: endpoint, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	strict, err := NewZoneHandler(store, client, "primary", time.Second, auditFailures, &strictFallback{})
	if err != nil {
		t.Fatalf("NewZoneHandler: %v", err)
	}
	return api.HandlerWithOptions(NewGeneratedServer(strict), api.StdHTTPServerOptions{BaseURL: "/api/v1"})
}

type zoneStoreStub struct {
	mu              sync.Mutex
	prepared        bool
	ensure          func(context.Context, database.Actor, database.ZoneBindingInput) (database.ZoneBinding, error)
	get             func(context.Context, database.Actor, string) (database.ZoneBinding, error)
	observe         func(context.Context, database.Actor, database.ZoneObservationInput) (database.ZoneObservation, error)
	retry           func(context.Context, database.Actor, database.ZoneDeletionInput) (database.ZoneDeletionPlan, error)
	deletionOutcome func(context.Context, database.ZoneDeletionPlan, database.ZoneDeletionOutcomeInput) (database.DNSOutcome, error)
}

func (store *zoneStoreStub) EnsureZoneBinding(ctx context.Context, actor database.Actor, input database.ZoneBindingInput) (database.ZoneBinding, error) {
	if store.ensure == nil {
		return database.ZoneBinding{}, database.ErrNotFound
	}
	return store.ensure(ctx, actor, input)
}

func (store *zoneStoreStub) GetZoneBinding(ctx context.Context, actor database.Actor, id string) (database.ZoneBinding, error) {
	if store.get == nil {
		return database.ZoneBinding{}, database.ErrNotFound
	}
	return store.get(ctx, actor, id)
}

func (*zoneStoreStub) ListZoneBindings(context.Context, database.Actor, database.ZoneBindingListOptions) (database.ZoneBindingPage, error) {
	return database.ZoneBindingPage{}, database.ErrNotFound
}

func (store *zoneStoreStub) ObserveZoneBinding(ctx context.Context, actor database.Actor, input database.ZoneObservationInput) (database.ZoneObservation, error) {
	if store.observe == nil {
		return database.ZoneObservation{}, database.ErrNotFound
	}
	return store.observe(ctx, actor, input)
}

func (*zoneStoreStub) ConfirmZoneBindingAbsent(context.Context, database.Actor, string) (database.ZoneBinding, error) {
	return database.ZoneBinding{}, database.ErrNotFound
}

func (store *zoneStoreStub) PrepareZoneDeletionRetry(ctx context.Context, actor database.Actor, input database.ZoneDeletionInput) (database.ZoneDeletionPlan, error) {
	if store.retry == nil {
		return database.ZoneDeletionPlan{}, database.ErrNotFound
	}
	return store.retry(ctx, actor, input)
}

func (store *zoneStoreStub) RecordZoneDeletionOutcome(ctx context.Context, plan database.ZoneDeletionPlan, input database.ZoneDeletionOutcomeInput) (database.DNSOutcome, error) {
	if store.deletionOutcome == nil {
		return database.DNSOutcome{}, database.ErrNotFound
	}
	return store.deletionOutcome(ctx, plan, input)
}

func (*zoneStoreStub) RebindZoneBinding(context.Context, database.Actor, string, database.ZoneBindingInput) (database.ZoneBinding, error) {
	return database.ZoneBinding{}, database.ErrNotFound
}

func testZoneBinding(createdAt time.Time, retired bool) database.ZoneBinding {
	binding := database.ZoneBinding{
		ID: testBindingID, Generation: 1, Upstream: "primary", PowerDNSZoneID: "existing.example.", ZoneName: "existing.example.",
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
	}
	if retired {
		binding.RetiredAt = pgtype.Timestamptz{Time: createdAt.Add(time.Hour), Valid: true}
	}
	return binding
}

func (store *zoneStoreStub) GetZoneBindingRecovery(context.Context, database.Actor, string) (database.ZoneBindingRecovery, error) {
	return database.ZoneBindingRecovery{DeletionState: "no_attempt", Actions: []string{"observe"}}, nil
}
