package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/page"
)

func TestAuditExportTraversalAndTransportAbort(t *testing.T) {
	for _, failure := range []string{"", "revoked", "demoted", "read"} {
		t.Run(failure, func(t *testing.T) {
			var reads, checks atomic.Int32
			store := &auditStoreStub{list: func(ctx context.Context, actor database.Actor, options database.AuditListOptions) (database.AuditPage, error) {
				n := reads.Add(1)
				if !actor.Operator || options.Limit != 500 || options.Action == nil || *options.Action != "identity.create" {
					t.Errorf("options/actor=%+v %+v", options, actor)
				}
				if failure == "read" && n == 2 {
					return database.AuditPage{}, errors.New("database unavailable")
				}
				result := database.AuditPage{Items: make([]database.AuditRecord, 500)}
				for i := range result.Items {
					result.Items[i] = database.AuditRecord{ID: testIdentityID, Action: "identity.create", Details: json.RawMessage(`{}`)}
				}
				if n < 3 {
					result.Next = &page.Key{CreatedAt: time.Now(), ID: testIdentityID}
				}
				return result, nil
			}}
			auth := authenticatorFunc(func(context.Context, string) (database.Actor, error) {
				n := checks.Add(1)
				if failure == "revoked" && n >= 3 {
					return database.Actor{}, database.ErrUnauthenticated
				}
				return database.Actor{IdentityID: testIdentityID, TokenID: testTokenID, Operator: failure != "demoted" || n < 3}, nil
			})
			exporter := &auditExportServer{store: store}
			endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				action := "identity.create"
				exporter.ExportAuditEvents(w, r, api.ExportAuditEventsParams{Action: &action})
			})
			boundary, err := NewBoundary(BoundaryConfig{MaxBodyBytes: 4096, RequestTimeout: time.Second, MaxConcurrent: 2})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(boundary(Authentication(auth)(endpoint)))
			defer server.Close()
			req, err := http.NewRequest(http.MethodGet, server.URL+auditExportPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-API-Key", validToken())
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, readErr := io.ReadAll(response.Body)
			if response.StatusCode != 200 {
				t.Fatalf("status %d body %s", response.StatusCode, body)
			}
			if failure == "" {
				if readErr != nil || strings.Count(string(body), "\n") != 1500 {
					t.Fatalf("complete rows=%d err=%v", strings.Count(string(body), "\n"), readErr)
				}
				if response.Header.Get("Content-Type") != "application/x-ndjson" || !strings.Contains(response.Header.Get("Content-Disposition"), "attachment") {
					t.Fatalf("headers=%v", response.Header)
				}
			} else {
				if readErr == nil || strings.Contains(string(body), `"error"`) {
					t.Fatalf("partial export ended successfully: rows=%d err=%v", strings.Count(string(body), "\n"), readErr)
				}
				if failure != "read" && reads.Load() != 1 {
					t.Fatalf("read unauthorized page: %d", reads.Load())
				}
			}
		})
	}
}

// exportWriter supplies only the native capabilities this route requires.
type exportWriter struct {
	*httptest.ResponseRecorder
	writeErr  error
	cancel    context.CancelFunc
	deadlines []time.Time
}

func (w *exportWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}
func (w *exportWriter) Write(data []byte) (int, error) {
	if w.cancel != nil {
		w.cancel()
	}
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseRecorder.Write(data)
}

func TestAuditExportStopsOnWriteFailureAndCancellation(t *testing.T) {
	for _, failure := range []string{"write", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			exporter := &auditExportServer{store: &auditStoreStub{list: func(context.Context, database.Actor, database.AuditListOptions) (database.AuditPage, error) {
				calls++
				return database.AuditPage{Items: []database.AuditRecord{{ID: testIdentityID}}, Next: &page.Key{CreatedAt: time.Now(), ID: testIdentityID}}, nil
			}}}
			writer := &exportWriter{ResponseRecorder: httptest.NewRecorder()}
			if failure == "write" {
				writer.writeErr = io.ErrClosedPipe
			} else {
				writer.cancel = cancel
			}
			actor := database.Actor{IdentityID: testIdentityID, TokenID: testTokenID, Operator: true}
			ctx = context.WithValue(ctx, reauthenticateKey, reauthenticate(func(context.Context) (database.Actor, error) { return actor, nil }))
			request := httptest.NewRequest("GET", auditExportPath, nil).WithContext(ctx)
			defer func() {
				if p := recover(); p != http.ErrAbortHandler {
					t.Errorf("panic=%v want ErrAbortHandler", p)
				}
				if calls != 1 {
					t.Errorf("read calls=%d", calls)
				}
			}()
			exporter.ExportAuditEvents(writer, request, api.ExportAuditEventsParams{})
			t.Fatal("incomplete transfer returned normally")
		})
	}
}

func TestAuditExportInitialStagesAndEachPageRemainBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, stage := range []string{"authentication", "compatibility"} {
			boundary, err := NewBoundary(BoundaryConfig{MaxBodyBytes: 4096, RequestTimeout: time.Second, MaxConcurrent: 1})
			if err != nil {
				t.Fatal(err)
			}
			auth := authenticatorFunc(func(ctx context.Context, _ string) (database.Actor, error) {
				if stage == "authentication" {
					<-ctx.Done()
					return database.Actor{}, ctx.Err()
				}
				return database.Actor{Operator: true}, nil
			})
			handler := boundary(Authentication(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
				w.WriteHeader(http.StatusServiceUnavailable)
			})))
			request := httptest.NewRequest("GET", auditExportPath, nil)
			request.Header.Set("X-API-Key", validToken())
			before := time.Now()
			writer := httptest.NewRecorder()
			handler.ServeHTTP(writer, request)
			if time.Since(before) != time.Second || writer.Code != 503 {
				t.Fatalf("%s timeout elapsed=%s status=%d", stage, time.Since(before), writer.Code)
			}
		}
		boundary, err := NewBoundary(BoundaryConfig{MaxBodyBytes: 4096, RequestTimeout: time.Second, MaxConcurrent: 1})
		if err != nil {
			t.Fatal(err)
		}
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		handler := boundary(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release }))
		go func() {
			defer close(done)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/occupied", nil))
		}()
		<-entered
		before := time.Now()
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest("GET", auditExportPath, nil))
		if time.Since(before) != time.Second || writer.Code != 503 {
			t.Fatalf("admission timeout elapsed=%s status=%d", time.Since(before), writer.Code)
		}
		close(release)
		<-done
		// An authenticated page read gets its own finite timeout after initial stages.
		exporter := &auditExportServer{store: &auditStoreStub{list: func(ctx context.Context, _ database.Actor, _ database.AuditListOptions) (database.AuditPage, error) {
			<-ctx.Done()
			return database.AuditPage{}, ctx.Err()
		}}}
		ctx := context.WithValue(t.Context(), reauthenticateKey, reauthenticate(func(context.Context) (database.Actor, error) { return database.Actor{Operator: true}, nil }))
		writer2 := &exportWriter{ResponseRecorder: httptest.NewRecorder()}
		before = time.Now()
		exporter.ExportAuditEvents(writer2, httptest.NewRequest("GET", auditExportPath, nil).WithContext(ctx), api.ExportAuditEventsParams{})
		if time.Since(before) != auditExportPageTimeout || writer2.Code != 503 {
			t.Fatalf("page timeout elapsed=%s status=%d", time.Since(before), writer2.Code)
		}
	})
}

func TestAuthorizedAuditExportOutlivesOrdinaryRequestDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		boundary, err := NewBoundary(BoundaryConfig{MaxBodyBytes: 4096, RequestTimeout: time.Second, MaxConcurrent: 1})
		if err != nil {
			t.Fatal(err)
		}
		exporter := &auditExportServer{store: &auditStoreStub{list: func(ctx context.Context, _ database.Actor, _ database.AuditListOptions) (database.AuditPage, error) {
			time.Sleep(2 * time.Second)
			if err := ctx.Err(); err != nil {
				t.Errorf("page inherited short initial deadline: %v", err)
			}
			return database.AuditPage{}, nil
		}}}
		auth := authenticatorFunc(func(context.Context, string) (database.Actor, error) { return database.Actor{Operator: true}, nil })
		handler := boundary(Authentication(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			exporter.ExportAuditEvents(w, r, api.ExportAuditEventsParams{})
		})))
		request := httptest.NewRequest("GET", auditExportPath, nil)
		request.Header.Set("X-API-Key", validToken())
		writer := &exportWriter{ResponseRecorder: httptest.NewRecorder()}
		handler.ServeHTTP(writer, request)
		if writer.Code != 200 || len(writer.deadlines) != 2 {
			t.Fatalf("stream status=%d deadlines=%d", writer.Code, len(writer.deadlines))
		}
	})
}
