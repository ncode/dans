package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/page"
)

func TestIAMHandlerPreservesExplicitNullPatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	store := &iamStoreStub{
		patchIdentity: func(_ context.Context, actor database.Actor, id string, patch database.IdentityPatch) (database.Identity, error) {
			if actor.RequestID != testRequestID || id != testIdentityID {
				t.Errorf("actor/id = %+v %q", actor, id)
			}
			if !patch.DisplayName.Set || patch.DisplayName.Value != nil || patch.Enabled != nil || patch.Operator != nil {
				t.Errorf("patch = %+v", patch)
			}
			return database.Identity{
				ID: testIdentityID, Kind: "user", Handle: "alice", DisplayName: nil,
				Enabled: true, CreatedAt: timestamp(now), UpdatedAt: timestamp(now),
			}, nil
		},
	}
	handler := newTestIAMServer(t, store)
	request := actorRequest(http.MethodPatch, "/api/v1/dans/identities/"+testIdentityID, []byte(`{"display_name":null}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	value, present := body["display_name"]
	if !present || value != nil {
		t.Errorf("display_name = %#v, present=%t; want explicit null", value, present)
	}
}

func TestManagementErrorsHaveStableInvisibleResourceAndConflictStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "unauthenticated", err: database.ErrUnauthenticated, status: http.StatusUnauthorized, body: "unauthorized"},
		{name: "forbidden", err: database.ErrForbidden, status: http.StatusForbidden, body: "forbidden"},
		{name: "invisible or absent", err: database.ErrNotFound, status: http.StatusNotFound, body: "not found"},
		{name: "conflict", err: database.ErrConflict, status: http.StatusConflict, body: "conflict"},
		{name: "invalid", err: database.ErrInvalid, status: http.StatusUnprocessableEntity, body: "unprocessable entity"},
		{name: "audit unavailable", err: database.ErrAuditUnavailable, status: http.StatusServiceUnavailable, body: "service unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, status := managementError(tt.err)
			if status != tt.status || body.Error != tt.body || body.Errors != nil {
				t.Errorf("managementError(%v) = %d %+v", tt.err, status, body)
			}
		})
	}
}

func TestIAMHandlerReturnsOneTimeSelfServiceTokenSecret(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	store := &iamStoreStub{
		createToken: func(_ context.Context, actor database.Actor, identityID string, input database.TokenCreate) (database.CreatedToken, error) {
			if identityID != testIdentityID || input.Label != "automation" || actor.RequestID != testRequestID {
				t.Errorf("CreateToken = %+v %q %+v", actor, identityID, input)
			}
			return database.CreatedToken{
				Token: database.TokenMetadata{
					ID: testTokenID, IdentityID: testIdentityID, Label: input.Label, Status: "active", CreatedAt: timestamp(now),
				},
				Secret: "dans_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			}, nil
		},
	}
	handler := newTestIAMServer(t, store)
	request := actorRequest(http.MethodPost, "/api/v1/dans/me/tokens", []byte(`{"label":"automation"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["secret"] != "dans_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		t.Errorf("secret = %#v", body["secret"])
	}
	if body["expires_at"] != nil || body["revoked_at"] != nil {
		t.Errorf("nullable timestamps = expires %#v revoked %#v", body["expires_at"], body["revoked_at"])
	}
	if got := response.Header().Get("Location"); got != "/api/v1/dans/me/tokens/"+testTokenID {
		t.Errorf("Location = %q", got)
	}
}

func TestIAMHandlerBindsCursorToListFilters(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	var calls int
	store := &iamStoreStub{
		listIdentities: func(_ context.Context, _ database.Actor, options database.IdentityListOptions) (database.IdentityPage, error) {
			calls++
			if options.Limit != 1 || options.Kind == nil || *options.Kind != "user" || options.Enabled == nil || !*options.Enabled || options.Handle == nil || *options.Handle != "alice" {
				t.Errorf("list options = %+v", options)
			}
			return database.IdentityPage{
				Items: []database.Identity{{
					ID: testIdentityID, Kind: "user", Handle: "alice", Enabled: true,
					CreatedAt: timestamp(now), UpdatedAt: timestamp(now),
				}},
				Next: &page.Key{CreatedAt: now, ID: testIdentityID},
			}, nil
		},
	}
	handler := newTestIAMServer(t, store)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, actorRequest(http.MethodGet, "/api/v1/dans/identities?limit=1&kind=user&enabled=true&handle=alice", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d; body=%s", first.Code, first.Body.String())
	}
	var firstBody struct {
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil || firstBody.NextCursor == nil {
		t.Fatalf("decode next cursor: %v %#v", err, firstBody.NextCursor)
	}

	query := url.Values{"cursor": {*firstBody.NextCursor}, "kind": {"service"}, "enabled": {"true"}, "handle": {"alice"}}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, actorRequest(http.MethodGet, "/api/v1/dans/identities?"+query.Encode(), nil))
	if second.Code != http.StatusUnprocessableEntity {
		t.Errorf("changed-filter cursor status = %d; body=%s", second.Code, second.Body.String())
	}
	if calls != 1 {
		t.Errorf("store calls = %d, want 1", calls)
	}
}

func TestIAMHandlerOwnsEveryIdentityGroupMembershipTokenAndMeRoute(t *testing.T) {
	t.Parallel()

	handler := newTestIAMServer(t, &iamStoreStub{})
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/dans/identities", ""},
		{http.MethodPost, "/api/v1/dans/identities", `{"kind":"user","handle":"alice"}`},
		{http.MethodGet, "/api/v1/dans/identities/" + testIdentityID, ""},
		{http.MethodPatch, "/api/v1/dans/identities/" + testIdentityID, `{}`},
		{http.MethodGet, "/api/v1/dans/identities/" + testIdentityID + "/tokens", ""},
		{http.MethodPost, "/api/v1/dans/identities/" + testIdentityID + "/tokens", `{"label":"automation"}`},
		{http.MethodDelete, "/api/v1/dans/identities/" + testIdentityID + "/tokens/" + testTokenID, ""},
		{http.MethodGet, "/api/v1/dans/groups", ""},
		{http.MethodPost, "/api/v1/dans/groups", `{"handle":"team"}`},
		{http.MethodGet, "/api/v1/dans/groups/" + testGroupID, ""},
		{http.MethodPatch, "/api/v1/dans/groups/" + testGroupID, `{}`},
		{http.MethodGet, "/api/v1/dans/groups/" + testGroupID + "/members", ""},
		{http.MethodPut, "/api/v1/dans/groups/" + testGroupID + "/members/" + testIdentityID, ""},
		{http.MethodDelete, "/api/v1/dans/groups/" + testGroupID + "/members/" + testIdentityID, ""},
		{http.MethodGet, "/api/v1/dans/me", ""},
		{http.MethodGet, "/api/v1/dans/me/groups", ""},
		{http.MethodGet, "/api/v1/dans/me/tokens", ""},
		{http.MethodPost, "/api/v1/dans/me/tokens", `{"label":"automation"}`},
		{http.MethodDelete, "/api/v1/dans/me/tokens/" + testTokenID, ""},
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

func newTestIAMServer(t *testing.T, store IAMStore) http.Handler {
	t.Helper()
	strict, err := NewIAMHandler(store, &strictFallback{})
	if err != nil {
		t.Fatalf("NewIAMHandler: %v", err)
	}
	return api.HandlerWithOptions(NewGeneratedServer(strict), api.StdHTTPServerOptions{BaseURL: "/api/v1"})
}

func actorRequest(method, target string, body []byte) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	actor := database.Actor{
		IdentityID: testIdentityID, TokenID: testTokenID, Kind: "user", Handle: "alice",
	}
	ctx := context.WithValue(WithRequestID(request.Context(), testRequestID), actorKey, actor)
	return request.WithContext(ctx)
}

type strictFallback struct{ api.StrictServerInterface }

type iamStoreStub struct {
	IAMStore
	patchIdentity  func(context.Context, database.Actor, string, database.IdentityPatch) (database.Identity, error)
	createToken    func(context.Context, database.Actor, string, database.TokenCreate) (database.CreatedToken, error)
	listIdentities func(context.Context, database.Actor, database.IdentityListOptions) (database.IdentityPage, error)
}

func (store *iamStoreStub) CreateIdentity(ctx context.Context, actor database.Actor, input database.IdentityCreate) (database.Identity, error) {
	return database.Identity{}, database.ErrNotFound
}

func (store *iamStoreStub) GetIdentity(ctx context.Context, actor database.Actor, id string) (database.Identity, error) {
	return database.Identity{}, database.ErrNotFound
}

func (store *iamStoreStub) PatchIdentity(ctx context.Context, actor database.Actor, id string, patch database.IdentityPatch) (database.Identity, error) {
	if store.patchIdentity == nil {
		return database.Identity{}, database.ErrNotFound
	}
	return store.patchIdentity(ctx, actor, id, patch)
}

func (store *iamStoreStub) CreateGroup(ctx context.Context, actor database.Actor, input database.GroupCreate) (database.Group, error) {
	return database.Group{}, database.ErrNotFound
}

func (store *iamStoreStub) GetGroup(ctx context.Context, actor database.Actor, id string) (database.Group, error) {
	return database.Group{}, database.ErrNotFound
}

func (store *iamStoreStub) ListGroups(ctx context.Context, actor database.Actor, options database.GroupListOptions) (database.GroupPage, error) {
	return database.GroupPage{}, database.ErrNotFound
}

func (store *iamStoreStub) PatchGroup(ctx context.Context, actor database.Actor, id string, patch database.GroupPatch) (database.Group, error) {
	return database.Group{}, database.ErrNotFound
}

func (store *iamStoreStub) AddGroupMember(ctx context.Context, actor database.Actor, groupID, identityID string) (database.GroupMembership, error) {
	return database.GroupMembership{}, database.ErrNotFound
}

func (store *iamStoreStub) RemoveGroupMember(ctx context.Context, actor database.Actor, groupID, identityID string) error {
	return database.ErrNotFound
}

func (store *iamStoreStub) ListGroupMembers(ctx context.Context, actor database.Actor, groupID string, options database.IdentityListOptions) (database.IdentityPage, error) {
	return database.IdentityPage{}, database.ErrNotFound
}

func (store *iamStoreStub) ListCurrentIdentityGroups(ctx context.Context, actor database.Actor, options database.GroupListOptions) (database.GroupPage, error) {
	return database.GroupPage{}, database.ErrNotFound
}

func (store *iamStoreStub) GetCurrentIdentity(ctx context.Context, actor database.Actor) (database.Identity, error) {
	return database.Identity{}, database.ErrNotFound
}

func (store *iamStoreStub) CreateToken(ctx context.Context, actor database.Actor, identityID string, input database.TokenCreate) (database.CreatedToken, error) {
	if store.createToken == nil {
		return database.CreatedToken{}, database.ErrNotFound
	}
	return store.createToken(ctx, actor, identityID, input)
}

func (store *iamStoreStub) ListTokens(ctx context.Context, actor database.Actor, identityID string, options database.TokenListOptions) (database.TokenPage, error) {
	return database.TokenPage{}, database.ErrNotFound
}

func (store *iamStoreStub) RevokeToken(ctx context.Context, actor database.Actor, identityID, tokenID string) error {
	return database.ErrNotFound
}

func (store *iamStoreStub) ListIdentities(ctx context.Context, actor database.Actor, options database.IdentityListOptions) (database.IdentityPage, error) {
	if store.listIdentities == nil {
		return database.IdentityPage{}, database.ErrNotFound
	}
	return store.listIdentities(ctx, actor, options)
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

const (
	testRequestID  = "00000000-0000-4000-8000-000000000001"
	testIdentityID = "00000000-0000-4000-8000-000000000010"
	testTokenID    = "00000000-0000-4000-8000-000000000020"
	testGroupID    = "00000000-0000-4000-8000-000000000030"
)
