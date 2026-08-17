package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	contractapi "github.com/ncode/dans/api/openapi"
	"github.com/ncode/dans/internal/database"
)

func TestRootHealthIsAnonymousAndDocsRequireAuthentication(t *testing.T) {
	t.Parallel()

	health, err := NewHealth(passingDependencies(), time.Second)
	if err != nil {
		t.Fatalf("NewHealth: %v", err)
	}
	root, err := NewRootHandler(health, contractapi.JSON(), http.NotFoundHandler())
	if err != nil {
		t.Fatalf("NewRootHandler: %v", err)
	}
	contractValidation := newTestContract(t)
	authentication := Authentication(authenticatorFunc(func(_ context.Context, token string) (database.Actor, error) {
		if token != validToken() {
			return database.Actor{}, database.ErrUnauthenticated
		}
		return database.Actor{IdentityID: "00000000-0000-4000-8000-000000000010"}, nil
	}))
	handler := Chain(root, contractValidation, authentication, Authorization(&recordingDenialAuditor{}, &recordingAuditFailureReporter{}))

	for _, path := range []string{"/livez", "/readyz"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
			t.Errorf("%s response = %d %q", path, recorder.Code, recorder.Body.String())
		}
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/docs", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized docs status = %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	request.Header.Set("X-API-Key", validToken())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("docs status = %d body=%q", recorder.Code, recorder.Body.String())
	}
	if !bytes.Equal(recorder.Body.Bytes(), contractapi.JSON()) {
		t.Error("docs response differs from exact combined contract")
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q", contentType)
	}
}

func TestRootHandlerDelegatesOnlyNonRootOperations(t *testing.T) {
	t.Parallel()

	health, err := NewHealth(passingDependencies(), time.Second)
	if err != nil {
		t.Fatalf("NewHealth: %v", err)
	}
	var called bool
	handler, err := NewRootHandler(health, contractapi.JSON(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	if err != nil {
		t.Fatalf("NewRootHandler: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil))
	if !called || recorder.Code != http.StatusTeapot {
		t.Errorf("delegated = %t status=%d", called, recorder.Code)
	}
}
