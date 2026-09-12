//go:build integration

package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ncode/dans/internal/benchtest"
	"github.com/ncode/dans/internal/database"
)

func TestBrowserApplicationPersistsCookiesAndFailsClosed(t *testing.T) {
	t.Parallel()
	conn, dsn := benchtest.NewPostgres(t)
	if err := database.Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	benchtest.Seed(t, conn)
	store := database.NewStore(conn)
	sessionConn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal("connect session store")
	}
	t.Cleanup(func() { _ = sessionConn.Close(context.Background()) })
	sessions := &BrowserSessions{Store: database.NewStore(sessionConn)}
	upstreamClient, _ := benchmarkUpstream(t)
	failures := &recordingAuditFailureReporter{}
	operations, err := NewDANSOperations(GeneratedConfig{Store: store, Upstream: upstreamClient, UpstreamID: "default", MutationTimeout: time.Second, AuditFailures: failures})
	if err != nil {
		t.Fatal(err)
	}
	application := benchmarkApplication(t, ApplicationConfig{
		Upstream: upstreamClient, Authenticator: store, Schema: func(ctx context.Context) error { return database.CheckRuntimeCompatibility(ctx, conn) },
		DANS: operations, Mutations: store, Lifecycle: store, Denials: store,
		BrowserSessions: sessions,
	})
	server := httptest.NewTLSServer(application)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, headers http.Header, want int) ([]byte, *http.Response) {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header = headers.Clone()
		if r.Header == nil {
			r.Header = make(http.Header)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal("application request failed")
		}
		defer response.Body.Close()
		result, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal("application response failed")
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.StatusCode, want, result)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Error("missing no-store")
		}
		return result, response
	}
	const sessionPath = "/api/v1/dans/session"
	const identityPath = "/api/v1/dans/me"
	login := `{"token":"` + benchtest.Token + `"}`
	for _, body := range []string{"{", `{"token":"invalid"}`, `{"token":42}`, `{"token":"one","token":"two"}`} {
		data, _ := request("POST", sessionPath, body, nil, 401)
		if string(data) != "{\"error\":\"unauthorized\"}\n" {
			t.Errorf("nonuniform login rejection: %s", data)
		}
	}
	request("POST", sessionPath, login, http.Header{"Sec-Fetch-Site": []string{"cross-site"}}, 403)
	data, response := request("POST", sessionPath, login, nil, 201)
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].MaxAge <= 0 || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("login did not set protected persistent cookie")
	}
	if bytes.Contains(data, []byte(benchtest.Token)) || bytes.Contains(data, []byte(cookies[0].Value)) {
		t.Fatal("sign-in exposed credential")
	}
	data, _ = request("GET", identityPath, "", nil, 200)
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &identity); err != nil || identity.ID != benchtest.IdentityID {
		t.Fatalf("cookie-authenticated identity missing: %v", err)
	}
	request("GET", identityPath, "", http.Header{"X-Api-Key": []string{benchtest.Token}}, 401)
	request("GET", identityPath, "", http.Header{"Cookie": []string{browserSessionCookie + "=" + cookies[0].Value}}, 401)
	request("DELETE", sessionPath, "", http.Header{"Origin": []string{"https://external.example"}}, 403)
	request("GET", identityPath, "", nil, 200)
	benchtest.Exec(t, conn, `UPDATE browser_sessions SET created_at=statement_timestamp()-interval '8 days',expires_at=statement_timestamp()-interval '1 day'`)
	request("GET", identityPath, "", nil, 401)
	_, response = request("DELETE", sessionPath, "", nil, 204)
	if len(response.Cookies()) != 1 || response.Cookies()[0].MaxAge != -1 {
		t.Fatal("expired cookie was not cleared")
	}
	request("GET", identityPath, "", http.Header{"X-Api-Key": []string{benchtest.Token}}, 200)
	request("POST", sessionPath, login, nil, 201)
	if err := sessionConn.Close(t.Context()); err != nil {
		t.Fatal("close session store")
	}
	_, response = request("DELETE", sessionPath, "", nil, 503)
	if len(response.Cookies()) != 1 || response.Cookies()[0].MaxAge != -1 {
		t.Fatal("failed logout did not clear browser cookie")
	}
	request("GET", identityPath, "", nil, 401)
	sessions.Store = store
	request("POST", sessionPath, login, nil, 201)
	benchtest.Exec(t, conn, `UPDATE schema_migrations SET checksum=repeat('0',64)`)
	request("POST", sessionPath, login, nil, 503)
	request("DELETE", sessionPath, "", nil, 204)
	request("GET", identityPath, "", nil, 401)
}
