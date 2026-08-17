package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/page"
)

func TestAuditHandlerReturnsOpaqueTargetsAndRequiredNulls(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC)
	store := &auditStoreStub{list: func(_ context.Context, actor database.Actor, options database.AuditListOptions) (database.AuditPage, error) {
		if actor.RequestID != testRequestID || options.TargetID == nil || *options.TargetID != "primary:example.org." || options.Action == nil || *options.Action != "powerdns.zone.patch" {
			t.Errorf("actor/options = %+v %+v", actor, options)
		}
		return database.AuditPage{
			Items: []database.AuditRecord{{
				ID: "00000000-0000-4000-8000-000000000060", OccurredAt: now,
				RequestID: testRequestID, Action: "powerdns.zone.patch", TargetKind: "powerdns_zone",
				TargetID: "primary:example.org.", Result: "denied", Details: json.RawMessage(`{"denied":[{"index":1}]}`),
			}},
			Next: &page.Key{CreatedAt: now, ID: "00000000-0000-4000-8000-000000000060"},
		}, nil
	}}
	handler := newTestAuditServer(t, store)
	request := actorRequest(http.MethodGet, "/api/v1/dans/audit-events?action=powerdns.zone.patch&target_id=primary%3Aexample.org.", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %#v", body["items"])
	}
	event, _ := items[0].(map[string]any)
	if event["target_id"] != "primary:example.org." {
		t.Errorf("target_id = %#v", event["target_id"])
	}
	if value, present := event["actor_id"]; !present || value != nil {
		t.Errorf("actor_id = %#v, present=%t", value, present)
	}
	if body["next_cursor"] == nil || body["next_cursor"] == "" {
		t.Errorf("next_cursor = %#v", body["next_cursor"])
	}
}

func TestAuditHandlerRejectsCursorWhenFiltersChange(t *testing.T) {
	t.Parallel()

	key := page.Key{CreatedAt: time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC), ID: "00000000-0000-4000-8000-000000000060"}
	cursor, err := page.EncodeCursor("audit-events", "action=identity.create", key)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	calls := 0
	store := &auditStoreStub{list: func(context.Context, database.Actor, database.AuditListOptions) (database.AuditPage, error) {
		calls++
		return database.AuditPage{}, nil
	}}
	handler := newTestAuditServer(t, store)
	request := actorRequest(http.MethodGet, "/api/v1/dans/audit-events?action=group.create&cursor="+cursor, nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
	if calls != 0 {
		t.Errorf("store calls = %d, want 0", calls)
	}
}

func newTestAuditServer(t *testing.T, store AuditStore) http.Handler {
	t.Helper()
	strict, err := NewAuditHandler(store, &strictFallback{})
	if err != nil {
		t.Fatalf("NewAuditHandler: %v", err)
	}
	return api.HandlerWithOptions(NewGeneratedServer(strict), api.StdHTTPServerOptions{BaseURL: "/api/v1"})
}

type auditStoreStub struct {
	list func(context.Context, database.Actor, database.AuditListOptions) (database.AuditPage, error)
}

func (store *auditStoreStub) ListAuditEvents(ctx context.Context, actor database.Actor, options database.AuditListOptions) (database.AuditPage, error) {
	if store.list == nil {
		return database.AuditPage{}, database.ErrNotFound
	}
	return store.list(ctx, actor, options)
}
