package httpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/page"
)

func TestAuditExportStopsAfterClientDisconnect(t *testing.T) {
	var reads atomic.Int32
	secondPageStarted := make(chan struct{})
	secondPageCanceled := make(chan error, 1)
	handlerDone := make(chan struct{})
	store := &auditStoreStub{list: func(ctx context.Context, _ database.Actor, _ database.AuditListOptions) (database.AuditPage, error) {
		switch reads.Add(1) {
		case 1:
			return database.AuditPage{
				Items: []database.AuditRecord{{ID: testIdentityID, Action: "identity.create", Details: json.RawMessage(`{}`)}},
				Next:  &page.Key{CreatedAt: time.Now(), ID: testIdentityID},
			}, nil
		case 2:
			close(secondPageStarted)
			<-ctx.Done()
			secondPageCanceled <- ctx.Err()
			return database.AuditPage{}, ctx.Err()
		default:
			return database.AuditPage{}, nil
		}
	}}
	exporter := &auditExportServer{store: store}
	auth := authenticatorFunc(func(context.Context, string) (database.Actor, error) {
		return database.Actor{IdentityID: testIdentityID, TokenID: testTokenID, Operator: true}, nil
	})
	boundary, err := NewBoundary(BoundaryConfig{MaxBodyBytes: 4096, RequestTimeout: time.Second, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		exporter.ExportAuditEvents(w, r, api.ExportAuditEventsParams{})
	})
	server := httptest.NewServer(boundary(Authentication(auth)(endpoint)))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 20 * time.Second
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+auditExportPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-API-Key", validToken())
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	line, err := bufio.NewReader(response.Body).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read first export row: %v", err)
	}
	var record database.AuditRecord
	if err := json.Unmarshal(line, &record); err != nil || record.ID != testIdentityID {
		t.Fatalf("first export row = %s, decode error = %v", line, err)
	}
	select {
	case <-secondPageStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second page read did not start")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("disconnect client: %v", err)
	}
	select {
	case err := <-secondPageCanceled:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("second page error = %v, want client cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second page read continued after disconnect")
	}
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("export handler did not exit after disconnect")
	}
	if got := reads.Load(); got != 2 {
		t.Errorf("page reads = %d, want 2", got)
	}
}
