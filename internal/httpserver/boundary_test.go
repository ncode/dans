package httpserver

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ncode/dans/internal/httpapi"
)

func TestBoundaryAssignsOwnedRequestIDSecurityHeadersAndLimitsBody(t *testing.T) {
	t.Parallel()

	const requestID = "00000000-0000-4000-8000-000000000001"
	boundary, err := NewBoundary(BoundaryConfig{
		MaxBodyBytes:   4,
		RequestTimeout: time.Second,
		MaxConcurrent:  2,
		NewRequestID: func() (string, error) {
			return requestID, nil
		},
	})
	if err != nil {
		t.Fatalf("NewBoundary: %v", err)
	}
	handler := boundary(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := RequestIDFromContext(request.Context()); got != requestID {
			t.Errorf("request ID = %q", got)
		}
		if _, ok := request.Context().Deadline(); !ok {
			t.Error("request context has no deadline")
		}
		body, err := io.ReadAll(request.Body)
		if !errors.As(err, new(*http.MaxBytesError)) {
			t.Errorf("read body error = %v", err)
		}
		if string(body) != "xxxx" {
			t.Errorf("bounded body = %q", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/dans/groups", io.NopCloser(&infiniteReader{}))
	request.Header.Set("X-Request-ID", "client-controlled")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d", recorder.Code)
	}
	wantHeaders := map[string]string{
		"X-Request-ID":           requestID,
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for name, value := range wantHeaders {
		if got := recorder.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestBoundaryRecoversPanicsAsSecretSafeError(t *testing.T) {
	t.Parallel()

	boundary, err := NewBoundary(BoundaryConfig{
		MaxBodyBytes:   1024,
		RequestTimeout: time.Second,
		MaxConcurrent:  1,
		NewRequestID:   func() (string, error) { return "00000000-0000-4000-8000-000000000001", nil },
	})
	if err != nil {
		t.Fatalf("NewBoundary: %v", err)
	}
	handler := boundary(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("panic-secret")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError || recorder.Body.String() != "{\"error\":\"internal server error\"}\n" {
		t.Errorf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestBoundaryAppliesOneRequestDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		boundary, err := NewBoundary(BoundaryConfig{
			MaxBodyBytes:   1024,
			RequestTimeout: 250 * time.Millisecond,
			MaxConcurrent:  1,
			NewRequestID:   func() (string, error) { return "00000000-0000-4000-8000-000000000001", nil },
		})
		if err != nil {
			t.Fatalf("NewBoundary: %v", err)
		}
		start := time.Now()
		handler := boundary(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
			httpapi.WriteError(w, RequestIDFromContext(request.Context()), httpapi.NewError(httpapi.KindGatewayTimeout, request.Context().Err()))
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if elapsed := time.Since(start); elapsed != 250*time.Millisecond {
			t.Errorf("elapsed = %s", elapsed)
		}
		if recorder.Code != http.StatusGatewayTimeout {
			t.Errorf("status = %d", recorder.Code)
		}
	})
}

func TestMiddlewareChainUsesDeclaredTrustBoundaryOrder(t *testing.T) {
	t.Parallel()

	var got []string
	middleware := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				got = append(got, name)
				next.ServeHTTP(w, request)
			})
		}
	}
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		got = append(got, "handler")
	}), middleware("request-boundary"), middleware("contract"), middleware("authentication"), middleware("authorization"), middleware("access-log"), middleware("response-security"))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	want := []string{"request-boundary", "contract", "authentication", "authorization", "access-log", "response-security", "handler"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestHTTPServerConfigurationBoundsHeadersAndConnectionTimes(t *testing.T) {
	t.Parallel()

	configuration := ServerConfig{
		Address:           "127.0.0.1:8080",
		MaxHeaderBytes:    16 << 10,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	server, err := NewHTTPServer(configuration, http.NotFoundHandler())
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}
	if server.Addr != configuration.Address || server.MaxHeaderBytes != configuration.MaxHeaderBytes || server.ReadHeaderTimeout != configuration.ReadHeaderTimeout || server.ReadTimeout != configuration.ReadTimeout || server.WriteTimeout != configuration.WriteTimeout || server.IdleTimeout != configuration.IdleTimeout {
		t.Errorf("server = %+v", server)
	}
}

type infiniteReader struct{}

func (*infiniteReader) Read(value []byte) (int, error) {
	for index := range value {
		value[index] = 'x'
	}
	return len(value), nil
}
