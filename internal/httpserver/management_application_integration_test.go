//go:build integration

package httpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/benchtest"
	"github.com/ncode/dans/internal/database"
)

func managementApplication(t *testing.T, store *database.Store) *httptest.Server {
	t.Helper()
	upstreamClient, _ := benchmarkUpstream(t)
	operations, err := NewDANSOperations(GeneratedConfig{Store: store, Upstream: upstreamClient, UpstreamID: "default", MutationTimeout: time.Second, AuditFailures: &recordingAuditFailureReporter{}})
	if err != nil {
		t.Fatal(err)
	}
	app := benchmarkApplication(t, ApplicationConfig{Upstream: upstreamClient, Authenticator: store, Schema: func(context.Context) error { return nil }, DANS: operations, Mutations: store, Lifecycle: store, Denials: store, BrowserSessions: &BrowserSessions{Store: store, DevelopmentHTTP: true}})
	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	return server
}

func managementRequest(t *testing.T, server *httptest.Server, method, path, body, credential string, want int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(credential, "session_") {
		req.AddCookie(&http.Cookie{Name: "dans_dev_session", Value: credential})
	} else {
		req.Header.Set("X-API-Key", credential)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("%s %s: status %d want %d body %s", method, path, response.StatusCode, want, data)
	}
	return data
}

func TestManagementApplicationRelationshipsAcrossInstances(t *testing.T) {
	t.Parallel()
	conn, dsn := benchtest.NewPostgres(t)
	if err := database.Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	benchtest.Seed(t, conn)
	benchtest.Exec(t, conn, "UPDATE identities SET is_operator=true WHERE id=$1", benchtest.IdentityID)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	firstStore := database.NewStore(pool)
	secondConn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer secondConn.Close(context.Background())
	first, second := managementApplication(t, firstStore), managementApplication(t, database.NewStore(secondConn))
	get := func(path string) []byte { return managementRequest(t, first, "GET", path, "", benchtest.Token, 200) }
	session, err := firstStore.CreateBrowserSession(t.Context(), benchtest.Token, "")
	if err != nil {
		t.Fatal(err)
	}
	metadata := managementRequest(t, first, "GET", "/api/v1/dans/me/credential", "", session.Secret, 200)
	if string(metadata) != "{\"token_id\":\""+benchtest.TokenID+"\"}\n" {
		t.Fatalf("credential metadata=%s", metadata)
	}
	targetBody := managementRequest(t, second, "POST", "/api/v1/dans/identities", `{"kind":"service","handle":"access_target"}`, benchtest.Token, 201)
	var target api.Identity
	if err := json.Unmarshal(targetBody, &target); err != nil {
		t.Fatal(err)
	}
	targetPath := "/api/v1/dans/identities/" + target.Id.String()
	for _, handle := range []string{"access_group", "access_group2"} {
		data := managementRequest(t, second, "POST", "/api/v1/dans/groups", `{"handle":"`+handle+`"}`, benchtest.Token, 201)
		var group api.Group
		if err := json.Unmarshal(data, &group); err != nil {
			t.Fatal(err)
		}
		managementRequest(t, second, "PUT", "/api/v1/dans/groups/"+group.Id.String()+"/members/"+target.Id.String(), "", benchtest.Token, 204)
	}
	var groups api.GroupPage
	if err := json.Unmarshal(get(targetPath+"/groups?limit=1&handle_prefix=access_"), &groups); err != nil {
		t.Fatal(err)
	}
	cursor, err := groups.NextCursor.Get()
	if err != nil || len(groups.Items) != 1 {
		t.Fatalf("groups page=%+v %v", groups, err)
	}
	managementRequest(t, first, "GET", targetPath+"/groups?handle_prefix=access&cursor="+string(cursor), "", benchtest.Token, 422)
	managementRequest(t, first, "GET", "/api/v1/dans/identities/"+benchtest.IdentityID+"/groups?handle_prefix=access_&cursor="+string(cursor), "", benchtest.Token, 422)
	get(targetPath + "/groups?handle_prefix=access_&cursor=" + string(cursor))
	managementRequest(t, second, "POST", "/api/v1/dans/delegations", `{"zone_binding_id":"`+benchtest.BindingID+`","identity_id":"`+target.Id.String()+`","selectors":[{"kind":"exact","value":"target.example.org."}]}`, benchtest.Token, 201)
	effective := get(targetPath + "/delegations")
	if !strings.Contains(string(effective), "target.example.org.") {
		t.Fatal("new delegation not visible across instances")
	}
	managementRequest(t, second, "PATCH", targetPath, `{"enabled":false}`, benchtest.Token, 200)
	if string(get(targetPath+"/delegations")) != "{\"items\":[],\"next_cursor\":null}\n" {
		t.Fatal("disabled target still has effective authority")
	}
	assignments := get(targetPath + "/assignments")
	if !strings.Contains(string(assignments), `"identity_enabled":false`) || !strings.Contains(string(assignments), `"effective":false`) || !strings.Contains(string(assignments), `"group_enabled":null`) {
		t.Fatalf("retained state=%s", assignments)
	}
	managementRequest(t, second, "PATCH", targetPath, `{"enabled":true}`, benchtest.Token, 200)
	if !strings.Contains(string(get(targetPath+"/delegations")), "target.example.org.") {
		t.Fatal("restored assignment absent")
	}
	tokenBody := managementRequest(t, second, "POST", targetPath+"/tokens", `{"label":"test-reader"}`, benchtest.Token, 201)
	var token api.TokenSecret
	if err := json.Unmarshal(tokenBody, &token); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"groups", "delegations", "assignments"} {
		managementRequest(t, first, "GET", targetPath+"/"+suffix, "", token.Secret, 403)
	}
}

type observedAuditStore struct {
	databaseStore *database.Store
	afterPage     func(int)
	calls         int
}

func (store *observedAuditStore) ListAuditEvents(ctx context.Context, actor database.Actor, options database.AuditListOptions) (database.AuditPage, error) {
	result, err := store.databaseStore.ListAuditEvents(ctx, actor, options)
	if err == nil {
		store.calls++
		if store.afterPage != nil {
			store.afterPage(len(result.Items))
		}
	}
	return result, err
}

func TestAuditExportReauthenticatesDurableCredentialEveryPage(t *testing.T) {
	for _, change := range []string{"token revoked", "token expired", "identity disabled", "operator demoted", "session revoked", "session expired"} {
		t.Run(change, func(t *testing.T) {
			conn, dsn := benchtest.NewPostgres(t)
			if err := database.Migrate(t.Context(), conn); err != nil {
				t.Fatal(err)
			}
			benchtest.Seed(t, conn)
			benchtest.Exec(t, conn, "UPDATE identities SET is_operator=true WHERE id=$1", benchtest.IdentityID)
			benchtest.Exec(t, conn, `INSERT INTO audit_events(id,event_kind,request_id,action,target_kind,target_id,result) SELECT ('60000000-0000-4000-8000-'||lpad(n::text,12,'0'))::uuid,'management','synthetic-export','synthetic.export','identity',$1,'succeeded' FROM generate_series(1,1001)n`, benchtest.IdentityID)
			other, err := pgx.Connect(t.Context(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close(context.Background())
			store := database.NewStore(conn)
			session, err := store.CreateBrowserSession(t.Context(), benchtest.Token, "")
			if err != nil {
				t.Fatal(err)
			}
			queries := map[string]string{
				"token revoked":     "UPDATE api_tokens SET revoked_at=clock_timestamp()",
				"token expired":     "UPDATE api_tokens SET created_at=clock_timestamp()-interval '2 hours', expires_at=clock_timestamp()-interval '1 hour'",
				"identity disabled": "UPDATE identities SET enabled=false",
				"operator demoted":  "UPDATE identities SET is_operator=false",
				"session revoked":   "DELETE FROM browser_sessions",
				"session expired":   "UPDATE browser_sessions SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 hour'",
			}
			observed := &observedAuditStore{databaseStore: store, afterPage: func(int) { benchtest.Exec(t, other, queries[change]) }}
			exporter := &auditExportServer{store: observed}
			boundary, err := NewBoundary(BoundaryConfig{MaxBodyBytes: 4096, RequestTimeout: time.Second, MaxConcurrent: 2})
			if err != nil {
				t.Fatal(err)
			}
			sessions := &BrowserSessions{DevelopmentHTTP: true}
			endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				exporter.ExportAuditEvents(w, r, api.ExportAuditEventsParams{})
			})
			server := httptest.NewServer(boundary(sessions.Authentication(store)(endpoint)))
			defer server.Close()
			request, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+auditExportPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(change, "session") {
				request.AddCookie(&http.Cookie{Name: "dans_dev_session", Value: session.Secret})
			} else {
				request.Header.Set("X-API-Key", benchtest.Token)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err == nil || strings.Count(string(body), "\n") != 500 || observed.calls != 1 {
				t.Fatalf("revocation %s: rows=%d calls=%d err=%v", change, strings.Count(string(body), "\n"), observed.calls, err)
			}
		})
	}
}

func TestManagementCapacityBoundedListsAndAuditExport(t *testing.T) {
	conn, dsn := benchtest.NewPostgres(t)
	if err := database.Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	benchtest.Seed(t, conn)
	benchtest.Exec(t, conn, "UPDATE identities SET is_operator=true WHERE id=$1", benchtest.IdentityID)
	seed, err := os.ReadFile("../benchtest/testdata/management.sql")
	if err != nil {
		t.Fatal(err)
	}
	benchtest.Exec(t, conn, string(seed))
	store := database.NewStore(conn)
	actor, err := store.Authenticate(t.Context(), benchtest.Token)
	if err != nil {
		t.Fatal(err)
	}
	actor.RequestID = "synthetic-capacity"
	prefix := "capacity-identity-"
	options := database.IdentityListOptions{Limit: 100, HandlePrefix: &prefix}
	total := 0
	for {
		result, err := store.ListIdentities(t.Context(), actor, options)
		if err != nil {
			t.Fatal(err)
		}
		total += len(result.Items)
		if len(result.Items) > 100 {
			t.Fatal("unbounded list")
		}
		if result.Next == nil {
			break
		}
		options.After = result.Next
	}
	if total != 1000 {
		t.Fatalf("identities=%d", total)
	}
	prefix = "capacity-identity-0001"
	result, err := store.ListIdentities(t.Context(), actor, database.IdentityListOptions{HandlePrefix: &prefix})
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("search beyond first page=%+v %v", result, err)
	}
	var identities, groups, members, tokens, audits int
	if err := conn.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM identities WHERE starts_with(handle,'capacity-')), (SELECT count(*) FROM groups), (SELECT count(*) FROM group_memberships), (SELECT count(*) FROM api_tokens WHERE starts_with(label,'capacity-')), (SELECT count(*) FROM audit_events)`).Scan(&identities, &groups, &members, &tokens, &audits); err != nil {
		t.Fatal(err)
	}
	if identities != 1000 || groups != 100 || members != 1000 || tokens != 100000 || audits != 100000 {
		t.Fatalf("capacity counts=%d/%d/%d/%d/%d", identities, groups, members, tokens, audits)
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var peak atomic.Uint64
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	observed := &observedAuditStore{databaseStore: database.NewStore(pool), afterPage: func(n int) {
		if n > 500 {
			t.Error("unbounded export page")
		}
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		for old := peak.Load(); stats.HeapAlloc > old; old = peak.Load() {
			if peak.CompareAndSwap(old, stats.HeapAlloc) {
				break
			}
		}
	}}
	boundary, err := NewBoundary(BoundaryConfig{MaxBodyBytes: 4096, RequestTimeout: time.Second, MaxConcurrent: 2})
	if err != nil {
		t.Fatal(err)
	}
	exporter := &auditExportServer{store: observed}
	server := httptest.NewServer(boundary(Authentication(database.NewStore(pool))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exporter.ExportAuditEvents(w, r, api.ExportAuditEventsParams{Action: new("synthetic.capacity")})
	}))))
	defer server.Close()
	req, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+auditExportPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", benchtest.Token)
	started := time.Now()
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	rows := 0
	for scanner.Scan() {
		var record database.AuditRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record.Action != "synthetic.capacity" {
			t.Fatal("unexpected audit row")
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows != 100000 || observed.calls != 200 {
		t.Fatalf("export rows=%d pages=%d", rows, observed.calls)
	}
	growth := int64(peak.Load()) - int64(baseline.HeapAlloc)
	if growth > 64<<20 {
		t.Fatalf("streaming heap grew %d bytes", growth)
	}
	t.Logf("capacity: identities=%d groups=%d members=%d tokens=%d audit_rows=%d pages=%d duration=%s sampled_heap_growth_bytes=%d", identities, groups, members, tokens, rows, observed.calls, time.Since(started), growth)
}
