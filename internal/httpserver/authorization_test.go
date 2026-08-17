package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ncode/dans/internal/contract"
	"github.com/ncode/dans/internal/database"
)

func TestRouteAuthorizationEnforcesExhaustiveAccessClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		class      contract.AccessClass
		actor      *database.Actor
		wantStatus int
	}{
		{name: "anonymous without actor", class: contract.AccessAnonymous, wantStatus: http.StatusNoContent},
		{name: "read ordinary identity", class: contract.AccessAuthenticatedRead, actor: &database.Actor{IdentityID: "reader"}, wantStatus: http.StatusNoContent},
		{name: "delegated ordinary identity reaches batch handler", class: contract.AccessDelegatedWrite, actor: &database.Actor{IdentityID: "delegate"}, wantStatus: http.StatusNoContent},
		{name: "delegated operator bypass", class: contract.AccessDelegatedWrite, actor: &database.Actor{IdentityID: "operator", Operator: true}, wantStatus: http.StatusNoContent},
		{name: "operator route ordinary identity", class: contract.AccessOperatorOnly, actor: &database.Actor{IdentityID: "reader"}, wantStatus: http.StatusForbidden},
		{name: "operator route operator", class: contract.AccessOperatorOnly, actor: &database.Actor{IdentityID: "operator", Operator: true}, wantStatus: http.StatusNoContent},
		{name: "read missing actor", class: contract.AccessAuthenticatedRead, wantStatus: http.StatusUnauthorized},
		{name: "unknown class fails closed", class: "future-class", actor: &database.Actor{IdentityID: "operator", Operator: true}, wantStatus: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			handler := Authorization(&recordingDenialAuditor{}, &recordingAuditFailureReporter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := context.WithValue(request.Context(), routeInfoKey, RouteInfo{OperationID: "operation", AccessClass: tt.class})
			metadata := &AccessMetadata{}
			ctx = context.WithValue(ctx, accessMetadataKey, metadata)
			if tt.actor != nil {
				ctx = context.WithValue(ctx, actorKey, *tt.actor)
			}
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body=%q", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			wantCalls := int64(0)
			if tt.wantStatus == http.StatusNoContent {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Errorf("handler calls = %d, want %d", calls.Load(), wantCalls)
			}
			if tt.actor != nil && metadata.actorID != tt.actor.IdentityID {
				t.Errorf("logged actor ID = %q", metadata.actorID)
			}
		})
	}
}

func TestRouteAuthorizationFailsClosedWithoutContractRoute(t *testing.T) {
	t.Parallel()

	handler := Authorization(&recordingDenialAuditor{}, &recordingAuditFailureReporter{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/undeclared", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d", recorder.Code)
	}
}

func TestRouteAuthorizationDurablyAuditsAuthenticatedOperatorDenial(t *testing.T) {
	t.Parallel()

	auditor := &recordingDenialAuditor{}
	handler := Authorization(auditor, &recordingAuditFailureReporter{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called")
	}))
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/localhost/zones/example.org.?api-key=secret", nil)
	ctx := context.WithValue(request.Context(), routeInfoKey, RouteInfo{
		Template: "/api/v1/servers/{server_id}/zones/{zone_id}", OperationID: "deleteZone", AccessClass: contract.AccessOperatorOnly,
	})
	ctx = context.WithValue(ctx, accessMetadataKey, &AccessMetadata{})
	ctx = context.WithValue(ctx, requestIDKey, "request-123")
	ctx = context.WithValue(ctx, actorKey, database.Actor{
		IdentityID: "00000000-0000-4000-8000-000000000001",
		TokenID:    "00000000-0000-4000-8000-000000000002",
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request.WithContext(ctx))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	actor, input := auditor.snapshot()
	if actor.RequestID != "request-123" || input.Action != "deleteZone" || input.TargetKind != "http_route" || input.TargetID != "/api/v1/servers/{server_id}/zones/{zone_id}" {
		t.Fatalf("denial audit = actor %+v, input %+v", actor, input)
	}
	if len(input.RequestDigest) != 32 {
		t.Errorf("request digest length = %d", len(input.RequestDigest))
	}
}

func TestRouteAuthorizationDeniesAndLatchesHealthWhenAuditFails(t *testing.T) {
	t.Parallel()

	auditor := &recordingDenialAuditor{err: errors.New("audit insert rejected")}
	failures := &recordingAuditFailureReporter{}
	handler := Authorization(auditor, failures)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called")
	}))
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := context.WithValue(request.Context(), routeInfoKey, RouteInfo{Template: "/api/v1/dans/groups", OperationID: "createGroup", AccessClass: contract.AccessOperatorOnly})
	ctx = context.WithValue(ctx, accessMetadataKey, &AccessMetadata{})
	ctx = context.WithValue(ctx, requestIDKey, "request-456")
	ctx = context.WithValue(ctx, actorKey, database.Actor{IdentityID: "identity", TokenID: "token"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request.WithContext(ctx))

	if recorder.Code != http.StatusForbidden || failures.count() != 1 {
		t.Fatalf("status = %d, failure reports = %d", recorder.Code, failures.count())
	}
}

type recordingDenialAuditor struct {
	mu    sync.Mutex
	err   error
	actor database.Actor
	input database.AuthorizationDenialInput
}

func (auditor *recordingDenialAuditor) RecordAuthorizationDenial(_ context.Context, actor database.Actor, input database.AuthorizationDenialInput) error {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	auditor.actor, auditor.input = actor, input
	return auditor.err
}

func (auditor *recordingDenialAuditor) snapshot() (database.Actor, database.AuthorizationDenialInput) {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	return auditor.actor, auditor.input
}
