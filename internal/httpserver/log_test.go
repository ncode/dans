package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogUsesRouteAndOpaqueIDsWithoutBodiesOrCredentials(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := NewJSONLogger(&output, slog.LevelInfo)
	handler := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		metadata := AccessMetadataFromContext(request.Context())
		metadata.SetRoute("/api/v1/servers/{server_id}/zones/{zone_id}", "ListZone")
		metadata.SetOpaqueIDs("00000000-0000-4000-8000-000000000010", "00000000-0000-4000-8000-000000000020", "00000000-0000-4000-8000-000000000030")
		metadata.SetUpstreamOutcome("http_response")
		w.WriteHeader(http.StatusPartialContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/localhost/zones/private.example.?token=query-secret", strings.NewReader(`{"secret":"body-secret"}`))
	request.Header.Set("X-API-Key", "header-secret")
	request = request.WithContext(WithRequestID(request.Context(), "00000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log: %v\n%s", err, output.Bytes())
	}
	want := map[string]any{
		"msg":              "http request",
		"request_id":       "00000000-0000-4000-8000-000000000001",
		"method":           http.MethodPost,
		"route_template":   "/api/v1/servers/{server_id}/zones/{zone_id}",
		"api_operation_id": "ListZone",
		"status":           float64(http.StatusPartialContent),
		"actor_id":         "00000000-0000-4000-8000-000000000010",
		"resource_id":      "00000000-0000-4000-8000-000000000020",
		"operation_id":     "00000000-0000-4000-8000-000000000030",
		"upstream_outcome": "http_response",
	}
	for name, value := range want {
		if record[name] != value {
			t.Errorf("%s = %#v, want %#v", name, record[name], value)
		}
	}
	if duration, ok := record["duration_ms"].(float64); !ok || duration < 0 {
		t.Errorf("duration_ms = %#v", record["duration_ms"])
	}
	text := output.String()
	for _, secret := range []string{"query-secret", "header-secret", "body-secret", "private.example"} {
		if strings.Contains(text, secret) {
			t.Errorf("log exposed %q: %s", secret, text)
		}
	}
}

func TestAccessLogCapturesImplicitStatusAndPreservesResponseWriterControl(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := NewJSONLogger(&output, slog.LevelInfo)
	base := &controllableWriter{header: make(http.Header)}
	handler := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	handler.ServeHTTP(base, httptest.NewRequest(http.MethodGet, "/", nil))
	if !base.flushed {
		t.Error("wrapped ResponseWriter hid Flush")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if record["status"] != float64(http.StatusOK) {
		t.Errorf("status = %#v", record["status"])
	}
}

type controllableWriter struct {
	header  http.Header
	flushed bool
}

func (writer *controllableWriter) Header() http.Header      { return writer.header }
func (*controllableWriter) Write(value []byte) (int, error) { return len(value), nil }
func (*controllableWriter) WriteHeader(int)                 {}
func (writer *controllableWriter) Flush()                   { writer.flushed = true }
