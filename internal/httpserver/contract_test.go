package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	contractapi "github.com/ncode/dans/api/openapi"
	"github.com/ncode/dans/internal/contract"
)

func TestContractValidationHonorsGlobalAndOperationServerURLs(t *testing.T) {
	t.Parallel()

	middleware := newTestContract(t)
	tests := []struct {
		path          string
		wantStatus    int
		wantOperation string
		wantClass     contract.AccessClass
	}{
		{path: "/livez", wantStatus: http.StatusNoContent, wantOperation: "getLiveness", wantClass: contract.AccessAnonymous},
		{path: "/readyz", wantStatus: http.StatusNoContent, wantOperation: "getReadiness", wantClass: contract.AccessAnonymous},
		{path: "/api/docs", wantStatus: http.StatusNoContent, wantOperation: "getAPIDocument", wantClass: contract.AccessAuthenticatedRead},
		{path: "/api/v1/servers/localhost", wantStatus: http.StatusNoContent, wantOperation: "listServer", wantClass: contract.AccessOperatorOnly},
		{path: "/api/v1/livez", wantStatus: http.StatusNotFound},
		{path: "/api/v1/readyz", wantStatus: http.StatusNotFound},
		{path: "/api/v1/api/docs", wantStatus: http.StatusNotFound},
		{path: "/metrics", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				information, ok := RouteInfoFromContext(request.Context())
				if !ok {
					t.Error("route information missing")
				}
				if information.OperationID != tt.wantOperation || information.AccessClass != tt.wantClass {
					t.Errorf("route = %+v", information)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body=%q", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestContractValidationRejectsMalformedAndInvalidBeforeDownstream(t *testing.T) {
	t.Parallel()

	middleware := newTestContract(t)
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "malformed", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "duplicate property", body: `{"handle":"team","handle":"other"}`, wantStatus: http.StatusBadRequest},
		{name: "unknown property", body: `{"handle":"team","unknown":true}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "missing required", body: `{}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "wrong type", body: `{"handle":true}`, wantStatus: http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				calls.Add(1)
			}))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/dans/groups", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body=%q", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if calls.Load() != 0 {
				t.Errorf("downstream calls = %d", calls.Load())
			}
		})
	}
}

func TestContractValidationPreservesValidatedBodyAndPathParameters(t *testing.T) {
	t.Parallel()

	middleware := newTestContract(t)
	const body = `{"handle":"team"}`
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotBody, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(gotBody) != body {
			t.Errorf("body = %q", gotBody)
		}
		information, _ := RouteInfoFromContext(request.Context())
		if information.Template != "/api/v1/dans/groups" || len(information.PathParameters) != 0 {
			t.Errorf("route information = %+v", information)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/dans/groups", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d body=%q", recorder.Code, recorder.Body.String())
	}
}

func newTestContract(t *testing.T) Middleware {
	t.Helper()
	document, err := contractapi.Spec()
	if err != nil {
		t.Fatalf("GetSpec: %v", err)
	}
	middleware, err := NewContractValidation(document)
	if err != nil {
		t.Fatalf("NewContractValidation: %v", err)
	}
	return middleware
}
