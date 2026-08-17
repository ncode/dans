package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ncode/dans/internal/database"
)

type benchmarkResponseWriter struct {
	header http.Header
	status int
}

func (writer *benchmarkResponseWriter) Header() http.Header { return writer.header }

func (writer *benchmarkResponseWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return len(body), nil
}

func (writer *benchmarkResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

func BenchmarkAuthenticationMiddleware(b *testing.B) {
	actor := database.Actor{
		IdentityID: "00000000-0000-4000-8000-000000000010",
		TokenID:    "00000000-0000-4000-8000-000000000020",
		Kind:       "user",
		Handle:     "alice",
	}
	handler := Authentication(authenticatorFunc(func(context.Context, string) (database.Actor, error) {
		return actor, nil
	}))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, test := range []struct {
		name  string
		path  string
		token string
	}{
		{name: "liveness", path: "/livez"},
		{name: "authenticated", path: "/api/v1/servers", token: validToken()},
	} {
		b.Run(test.name, func(b *testing.B) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.token != "" {
				request.Header.Set("X-API-Key", test.token)
			}
			response := benchmarkResponseWriter{header: make(http.Header)}

			b.ReportAllocs()
			for b.Loop() {
				response.status = 0
				handler.ServeHTTP(&response, request)
			}
			if response.status != http.StatusNoContent {
				b.Fatalf("status = %d, want %d", response.status, http.StatusNoContent)
			}
		})
	}
}
