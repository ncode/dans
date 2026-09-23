package httpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/page"
)

type exportPipeListener struct {
	conn   net.Conn
	addr   net.Addr
	closed chan struct{}
	once   sync.Once
}

func (listener *exportPipeListener) Accept() (net.Conn, error) {
	if listener.conn != nil {
		conn := listener.conn
		listener.conn = nil
		return conn, nil
	}
	<-listener.closed
	return nil, net.ErrClosed
}

func (listener *exportPipeListener) Close() error {
	listener.once.Do(func() { close(listener.closed) })
	return nil
}

func (listener *exportPipeListener) Addr() net.Addr { return listener.addr }

func TestAuditExportStalledReaderAbortsTransport(t *testing.T) {
	var reads atomic.Int32
	items := make([]database.AuditRecord, 500)
	for i := range items {
		items[i] = database.AuditRecord{ID: testIdentityID, Action: "identity.create", Details: json.RawMessage(`{}`)}
	}
	store := &auditStoreStub{list: func(context.Context, database.Actor, database.AuditListOptions) (database.AuditPage, error) {
		reads.Add(1)
		return database.AuditPage{Items: items, Next: &page.Key{CreatedAt: time.Now(), ID: testIdentityID}}, nil
	}}
	exporter := &auditExportServer{store: store}
	done := make(chan struct{})
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer close(done)
		exporter.ExportAuditEvents(w, request, api.ExportAuditEventsParams{})
	})
	boundary, err := NewBoundary(BoundaryConfig{MaxBodyBytes: 4096, RequestTimeout: time.Second, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	auth := authenticatorFunc(func(context.Context, string) (database.Actor, error) {
		return database.Actor{IdentityID: testIdentityID, TokenID: testTokenID, Operator: true}, nil
	})
	clientConn, serverConn := net.Pipe()
	listener := &exportPipeListener{conn: serverConn, addr: serverConn.LocalAddr(), closed: make(chan struct{})}
	server := &http.Server{Handler: boundary(Authentication(auth)(endpoint))}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		_ = server.Close()
		<-serveDone
	})

	if err := clientConn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(clientConn, "GET "+auditExportPath+" HTTP/1.1\r\nHost: test\r\nX-API-Key: "+validToken()+"\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clientConn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	body := bufio.NewReader(response.Body)
	line, err := body.ReadString('\n')
	if err != nil || !json.Valid([]byte(line)) {
		t.Fatalf("first event = %q, err = %v", line, err)
	}
	if err := clientConn.SetDeadline(time.Now().Add(2*auditExportPageTimeout + 5*time.Second)); err != nil {
		t.Fatal(err)
	}
	stalledAt := time.Now()
	select {
	case <-done:
	case <-time.After(2 * auditExportPageTimeout):
		t.Fatal("stalled export did not stop within the write-deadline bound")
	}
	if elapsed := time.Since(stalledAt); elapsed < auditExportPageTimeout-5*time.Second {
		t.Errorf("stalled export stopped before its write deadline: %s", elapsed)
	}
	_, err = io.Copy(io.Discard, body)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("partial export read error = %v, want unexpected EOF", err)
	}
	if got := reads.Load(); got != 1 {
		t.Errorf("page reads = %d, want 1", got)
	}
}
