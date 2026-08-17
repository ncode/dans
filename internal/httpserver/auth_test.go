package httpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ncode/dans/internal/database"
)

func TestAuthenticationAcceptsExactlyOneUsableToken(t *testing.T) {
	t.Parallel()

	wantActor := database.Actor{
		IdentityID: "00000000-0000-4000-8000-000000000010",
		TokenID:    "00000000-0000-4000-8000-000000000020",
		Kind:       "user",
		Handle:     "alice",
		Operator:   true,
	}
	var calls atomic.Int64
	authenticator := authenticatorFunc(func(_ context.Context, token string) (database.Actor, error) {
		calls.Add(1)
		if token != validToken() {
			t.Errorf("token = %q", token)
		}
		return wantActor, nil
	})
	handler := Authentication(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		actor, ok := ActorFromContext(request.Context())
		if !ok || !reflect.DeepEqual(actor, wantActor) {
			t.Errorf("actor = %+v, %t", actor, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/localhost/zones", nil)
	request.Header.Set("X-API-Key", validToken())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d", recorder.Code)
	}
	if calls.Load() != 1 {
		t.Errorf("authentication calls = %d", calls.Load())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestAuthenticationReturnsUniformUnauthorizedWithoutCallingHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		headers    []string
		authResult error
		wantCalls  int64
	}{
		{name: "missing"},
		{name: "empty", headers: []string{""}},
		{name: "malformed", headers: []string{"not-a-token"}},
		{name: "comma combined", headers: []string{validToken() + "," + validToken()}},
		{name: "duplicate", headers: []string{validToken(), validToken()}},
		{name: "unknown", headers: []string{validToken()}, authResult: database.ErrUnauthenticated, wantCalls: 1},
		{name: "revoked", headers: []string{validToken()}, authResult: database.ErrUnauthenticated, wantCalls: 1},
		{name: "expired", headers: []string{validToken()}, authResult: database.ErrUnauthenticated, wantCalls: 1},
		{name: "disabled owner", headers: []string{validToken()}, authResult: database.ErrUnauthenticated, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var authCalls atomic.Int64
			var handlerCalls atomic.Int64
			authenticator := authenticatorFunc(func(context.Context, string) (database.Actor, error) {
				authCalls.Add(1)
				return database.Actor{}, tt.authResult
			})
			handler := Authentication(authenticator)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				handlerCalls.Add(1)
			}))
			request := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
			request = request.WithContext(WithRequestID(request.Context(), "00000000-0000-4000-8000-000000000001"))
			for _, value := range tt.headers {
				request.Header.Add("X-API-Key", value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("status = %d", recorder.Code)
			}
			if authCalls.Load() != tt.wantCalls {
				t.Errorf("authentication calls = %d, want %d", authCalls.Load(), tt.wantCalls)
			}
			if handlerCalls.Load() != 0 {
				t.Errorf("handler calls = %d", handlerCalls.Load())
			}
			if recorder.Body.String() != "{\"error\":\"unauthorized\"}\n" {
				t.Errorf("body = %q", recorder.Body.String())
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestAuthenticationMapsCurrentStateFailureToUnavailableWithoutSecret(t *testing.T) {
	t.Parallel()

	const internal = "postgres://user:database-secret@db.internal/dans"
	handler := Authentication(authenticatorFunc(func(context.Context, string) (database.Actor, error) {
		return database.Actor{}, errors.New(internal)
	}))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called")
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	request.Header.Set("X-API-Key", validToken())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	result := recorder.Result()
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if result.StatusCode != http.StatusServiceUnavailable || strings.Contains(string(body), internal) || strings.Contains(string(body), validToken()) {
		t.Errorf("response = %d %q", result.StatusCode, body)
	}
}

func TestAuthenticationSkipsOnlyRootHealthRoutes(t *testing.T) {
	t.Parallel()

	var authCalls atomic.Int64
	authenticator := authenticatorFunc(func(context.Context, string) (database.Actor, error) {
		authCalls.Add(1)
		return database.Actor{}, database.ErrUnauthenticated
	})
	handler := Authentication(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/livez", "/readyz"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNoContent {
			t.Errorf("%s status = %d", path, recorder.Code)
		}
	}
	for _, path := range []string{"/api/docs", "/api/v1/livez", "/api/v1/readyz"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-API-Key", validToken())
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d", path, recorder.Code)
		}
	}
	if authCalls.Load() != 3 {
		t.Errorf("authentication calls = %d, want 3", authCalls.Load())
	}
}

type authenticatorFunc func(context.Context, string) (database.Actor, error)

func (function authenticatorFunc) Authenticate(ctx context.Context, token string) (database.Actor, error) {
	return function(ctx, token)
}

func validToken() string {
	return "dans_v1_" + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
}
