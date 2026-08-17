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
	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestDelegationHandlerCreatesGeneratedRepresentation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 14, 0, 0, 0, time.UTC)
	store := &delegationStoreStub{
		create: func(_ context.Context, actor database.Actor, input database.DelegationCreate) (database.DelegationDetails, error) {
			if actor.RequestID != testRequestID || input.ZoneBindingID != testBindingID || input.GranteeIdentityID == nil || *input.GranteeIdentityID != testIdentityID || input.GranteeGroupID != nil {
				t.Errorf("create actor/input = %+v %+v", actor, input)
			}
			if len(input.Selectors) != 2 || input.Selectors[0].Kind != delegationdomain.SelectorExact || input.Selectors[1].Kind != delegationdomain.SelectorGlob {
				t.Errorf("selectors = %+v", input.Selectors)
			}
			return database.DelegationDetails{
				ID: testDelegationID, ZoneBindingID: testBindingID, GranteeKind: "identity", GranteeID: testIdentityID,
				Selectors: input.Selectors, RecordTypes: []string{"A", "PTR"}, ChangeKinds: []string{"REPLACE"}, CreatedAt: timestamp(now),
			}, nil
		},
	}
	handler := newTestDelegationServer(t, store)
	body := `{"zone_binding_id":"` + testBindingID + `","identity_id":"` + testIdentityID + `","selectors":[{"kind":"exact","value":"host.example."},{"kind":"glob","value":"*.example."}],"record_types":["A","PTR"],"change_kinds":["REPLACE"]}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, actorRequest(http.MethodPost, "/api/v1/dans/delegations", []byte(body)))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Location"); got != "/api/v1/dans/delegations/"+testDelegationID {
		t.Errorf("Location = %q", got)
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if value, ok := result["revoked_at"]; !ok || value != nil {
		t.Errorf("revoked_at = %#v, present=%t", value, ok)
	}
}

func TestDelegationHandlerOwnsOperatorAndSelfServiceRoutes(t *testing.T) {
	t.Parallel()

	handler := newTestDelegationServer(t, &delegationStoreStub{})
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/dans/delegations", ""},
		{http.MethodPost, "/api/v1/dans/delegations", `{"zone_binding_id":"` + testBindingID + `","identity_id":"` + testIdentityID + `","selectors":[{"kind":"exact","value":"host.example."}]}`},
		{http.MethodGet, "/api/v1/dans/delegations/" + testDelegationID, ""},
		{http.MethodDelete, "/api/v1/dans/delegations/" + testDelegationID, ""},
		{http.MethodGet, "/api/v1/dans/me/delegations", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, actorRequest(tt.method, tt.path, []byte(tt.body)))
			if response.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404; body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func newTestDelegationServer(t *testing.T, store DelegationStore) http.Handler {
	t.Helper()
	strict, err := NewDelegationHandler(store, &strictFallback{})
	if err != nil {
		t.Fatalf("NewDelegationHandler: %v", err)
	}
	return api.HandlerWithOptions(NewGeneratedServer(strict), api.StdHTTPServerOptions{BaseURL: "/api/v1"})
}

type delegationStoreStub struct {
	DelegationStore
	create func(context.Context, database.Actor, database.DelegationCreate) (database.DelegationDetails, error)
}

func (store *delegationStoreStub) CreateDelegation(ctx context.Context, actor database.Actor, input database.DelegationCreate) (database.DelegationDetails, error) {
	if store.create == nil {
		return database.DelegationDetails{}, database.ErrNotFound
	}
	return store.create(ctx, actor, input)
}

func (*delegationStoreStub) GetDelegation(context.Context, database.Actor, string) (database.DelegationDetails, error) {
	return database.DelegationDetails{}, database.ErrNotFound
}

func (*delegationStoreStub) ListDelegations(context.Context, database.Actor, database.DelegationListOptions) (database.DelegationPage, error) {
	return database.DelegationPage{}, database.ErrNotFound
}

func (*delegationStoreStub) ListCurrentIdentityDelegations(context.Context, database.Actor, database.DelegationListOptions) (database.DelegationPage, error) {
	return database.DelegationPage{}, database.ErrNotFound
}

func (*delegationStoreStub) RevokeDelegation(context.Context, database.Actor, string) error {
	return database.ErrNotFound
}

const (
	testBindingID    = "00000000-0000-4000-8000-000000000040"
	testDelegationID = "00000000-0000-4000-8000-000000000050"
)
