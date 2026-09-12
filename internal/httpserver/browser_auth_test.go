package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

func TestBrowserAuthenticationSelectsOneCredential(t *testing.T) {
	t.Parallel()
	secret := "session_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	for _, tt := range []struct {
		name    string
		headers []string
		cookies []string
		want    int
	}{
		{"session", nil, []string{secret}, 204},
		{"mixed", []string{validToken()}, []string{secret}, 401},
		{"duplicate session", nil, []string{secret, secret}, 401},
		{"malformed session", nil, []string{"invalid"}, 401},
		{"header session", []string{secret}, nil, 401},
		{"cookie token", nil, []string{validToken()}, 401},
		{"empty header with cookie", []string{""}, []string{secret}, 401},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := Authentication(authenticatorFunc(func(_ context.Context, value string) (database.Actor, error) {
				if value != secret {
					t.Errorf("selected wrong credential")
				}
				return database.Actor{IdentityID: "actor"}, nil
			}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := ActorFromContext(r.Context()); !ok {
					t.Error("missing authenticated actor")
				}
				w.WriteHeader(204)
			}))
			r := httptest.NewRequest("GET", "/api/v1/dans/me", nil)
			for _, value := range tt.headers {
				r.Header.Add("X-API-Key", value)
			}
			for _, value := range tt.cookies {
				r.AddCookie(&http.Cookie{Name: "__Host-dans_session", Value: value})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
			if tt.want == 401 && w.Body.String() != "{\"error\":\"unauthorized\"}\n" {
				t.Errorf("nonuniform rejection: %s", w.Body.String())
			}
		})
	}
}

func TestBrowserAuthenticationRejectsCrossOriginBeforeCallingDependency(t *testing.T) {
	t.Parallel()
	h := Authentication(authenticatorFunc(func(context.Context, string) (database.Actor, error) {
		t.Error("cross-origin request reached authentication dependency")
		return database.Actor{}, nil
	}))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("cross-origin request reached handler") }))
	for _, path := range []string{"/api/v1/dans/session", "/api/v1/dans/identities", "/api/v1/servers/localhost/zones/example.org."} {
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.AddCookie(&http.Cookie{Name: "__Host-dans_session", Value: "session_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	}
}

type browserSessionStoreFake struct {
	create func(context.Context, string, string) (database.CreatedBrowserSession, error)
	remove func(context.Context, string) error
}

func (s browserSessionStoreFake) CreateBrowserSession(ctx context.Context, token, previous string) (database.CreatedBrowserSession, error) {
	return s.create(ctx, token, previous)
}
func (s browserSessionStoreFake) DeleteBrowserSession(ctx context.Context, secret string) error {
	return s.remove(ctx, secret)
}

func TestDevelopmentBrowserSessionWorksWithoutSecureCookieSupport(t *testing.T) {
	const secret = "session_v1_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA"
	sessions := &BrowserSessions{
		DevelopmentHTTP: true,
		Store: browserSessionStoreFake{
			create: func(context.Context, string, string) (database.CreatedBrowserSession, error) {
				return database.CreatedBrowserSession{Secret: secret, ExpiresAt: time.Now().Add(time.Hour)}, nil
			},
			remove: func(_ context.Context, value string) error {
				if value != secret {
					t.Error("logout did not invalidate the development session")
				}
				return nil
			},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/dans/session", sessions.CreateBrowserSession)
	mux.HandleFunc("DELETE /api/v1/dans/session", sessions.DeleteBrowserSession)
	mux.HandleFunc("GET /api/v1/dans/me", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	authenticator := authenticatorFunc(func(_ context.Context, value string) (database.Actor, error) {
		if value != secret {
			return database.Actor{}, database.ErrUnauthenticated
		}
		return database.Actor{}, nil
	})
	server := httptest.NewServer(sessions.Authentication(authenticator)(mux))
	t.Cleanup(server.Close)
	client := server.Client()
	var err error
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string, want int) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("%s %s: status %d, want %d", method, path, response.StatusCode, want)
		}
		return response
	}
	response := call("POST", "/api/v1/dans/session", `{"token":"`+validToken()+`"}`, 201)
	call("GET", "/api/v1/dans/me", "", 204)
	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "dans_dev_session" || cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge <= 0 {
		t.Fatal("incorrect development cookie attributes")
	}
	for _, tc := range []struct {
		name    string
		headers http.Header
		want    int
	}{
		{"cross-origin", http.Header{"Sec-Fetch-Site": []string{"cross-site"}}, 403},
		{"ambiguous", http.Header{"X-API-Key": []string{validToken()}}, 401},
		{"duplicate", http.Header{"Cookie": []string{"dans_dev_session=" + secret}}, 401},
	} {
		r, err := http.NewRequestWithContext(t.Context(), "POST", server.URL+"/api/v1/dans/me", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header = tc.headers
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.want {
			t.Errorf("%s status=%d want=%d", tc.name, response.StatusCode, tc.want)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/dans/me", nil)
	r.AddCookie(cookies[0])
	w := httptest.NewRecorder()
	Authentication(authenticator)(mux).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("secure default accepted a development cookie")
	}
	call("DELETE", "/api/v1/dans/session", "", 204)
	call("GET", "/api/v1/dans/me", "", 401)
}

func TestBrowserSessionHandlersProtectSecretsAndClearExpiredLogin(t *testing.T) {
	t.Parallel()
	const secret = "session_v1_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA"
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	store := browserSessionStoreFake{
		create: func(_ context.Context, token, previous string) (database.CreatedBrowserSession, error) {
			if token != validToken() || previous != "" {
				t.Error("wrong sign-in inputs")
			}
			return database.CreatedBrowserSession{Secret: secret, ExpiresAt: expires}, nil
		},
		remove: func(_ context.Context, value string) error {
			if value != secret {
				t.Error("wrong sign-out credential")
			}
			return nil
		},
	}
	sessions := &BrowserSessions{Store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/dans/session", sessions.CreateBrowserSession)
	mux.HandleFunc("DELETE /api/v1/dans/session", sessions.DeleteBrowserSession)
	h := Authentication(nil)(mux)
	r := httptest.NewRequest("POST", "/api/v1/dans/session", strings.NewReader(`{"token":"`+validToken()+`"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("sign-in=%d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	c := cookies[0]
	if c.Name != "__Host-dans_session" || c.Value != secret || !c.Secure || !c.HttpOnly || c.Domain != "" || c.Path != "/" || c.SameSite != http.SameSiteStrictMode || c.MaxAge <= 0 || c.MaxAge > 604800 || !c.Expires.Equal(expires) {
		t.Errorf("incorrect cookie protection")
	}
	if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), validToken()) {
		t.Fatal("credential exposed in JSON")
	}
	var metadata struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil || !metadata.ExpiresAt.Equal(expires) {
		t.Errorf("expiry metadata=%v err=%v", metadata, err)
	}
	r = httptest.NewRequest("DELETE", "/api/v1/dans/session", nil)
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Errorf("sign-out=%d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("DELETE", "/api/v1/dans/session", nil)
	r.AddCookie(&http.Cookie{Name: c.Name, Value: "malformed"})
	store.remove = func(context.Context, string) error { return nil }
	sessions.Store = store
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Errorf("unusable sign-out=%d", w.Code)
	}
}

func TestBrowserSignInRejectsMalformedOrAmbiguousCredentials(t *testing.T) {
	t.Parallel()
	sessions := &BrowserSessions{Store: browserSessionStoreFake{create: func(context.Context, string, string) (database.CreatedBrowserSession, error) {
		t.Error("invalid credential reached store")
		return database.CreatedBrowserSession{}, nil
	}}}
	for _, tt := range []struct{ body, header, cookie string }{
		{`{"token":"bad"}`, "", ""},
		{`{"token":"` + validToken() + `","token":"` + validToken() + `"}`, "", ""},
		{`{"token":"` + validToken() + `"}`, validToken(), ""},
		{`{"token":"` + validToken() + `"}`, "", "__Host-dans_session=one; __Host-dans_session=two"},
	} {
		r := httptest.NewRequest("POST", "/api/v1/dans/session", strings.NewReader(tt.body))
		if tt.header != "" {
			r.Header.Set("X-API-Key", tt.header)
		}
		if tt.cookie != "" {
			r.Header.Set("Cookie", tt.cookie)
		}
		w := httptest.NewRecorder()
		sessions.CreateBrowserSession(w, r)
		if w.Code != 401 || w.Body.String() != "{\"error\":\"unauthorized\"}\n" {
			t.Errorf("rejection=%d %s", w.Code, w.Body.String())
		}
	}
}

type browserMutationStore struct {
	recordingMutationStore
	authorize func(string) (database.AuthorizationDecision, error)
}

func (s *browserMutationStore) AuthorizeRRsetBatch(_ context.Context, credential, _, _, _ string, _ []database.RRsetTuple) (database.AuthorizationDecision, error) {
	return s.authorize(credential)
}

func TestBrowserPatchRevalidatesSessionAndKeepsCookieLocal(t *testing.T) {
	t.Parallel()
	const secret = "session_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-API-Key") != "upstream-key" {
			t.Error("client credential forwarded")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatal(err)
	}
	revoked := false
	mutations := &browserMutationStore{authorize: func(credential string) (database.AuthorizationDecision, error) {
		if credential != secret {
			t.Error("delegated decision lost the browser credential")
		}
		if revoked {
			return database.AuthorizationDecision{}, database.ErrUnauthenticated
		}
		return database.AuthorizationDecision{Allowed: true, Actor: database.Actor{IdentityID: "00000000-0000-4000-8000-000000000001", TokenID: "00000000-0000-4000-8000-000000000002", RequestID: "browser-patch"}}, nil
	}}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: mutations, UpstreamID: "default", MutationTimeout: time.Second}
	for _, mode := range []struct {
		development bool
		name        string
	}{{false, browserSessionCookie}, {true, "dans_dev_session"}} {
		proxy.BrowserSessions = &BrowserSessions{DevelopmentHTTP: mode.development}
		revoked = false
		for _, want := range []int{204, 401} {
			r := httptest.NewRequest("PATCH", "/api/v1/servers/localhost/zones/example.org.", strings.NewReader(`{"rrsets":[{"name":"www.example.org.","type":"A","changetype":"DELETE"}]}`))
			r = r.WithContext(WithRequestID(r.Context(), "browser-patch"))
			r.AddCookie(&http.Cookie{Name: mode.name, Value: secret})
			w := httptest.NewRecorder()
			proxy.PatchZone(w, r, api.ServerId("localhost"), api.ZoneId("example.org."))
			if w.Code != want {
				t.Errorf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
			revoked = true
		}
	}
	if calls.Load() != 2 {
		t.Errorf("upstream writes=%d, want2", calls.Load())
	}
}

func TestBrowserSessionContractPreservesUniformLoginErrors(t *testing.T) {
	t.Parallel()
	sessions := &BrowserSessions{}
	h := newTestContract(t)(Authentication(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := RouteInfoFromContext(r.Context())
		if !ok || route.OperationID != "createBrowserSession" {
			t.Error("session route was not classified")
		}
		sessions.CreateBrowserSession(w, r)
	})))
	for _, body := range []string{"{", `{"token":"bad"}`, `{"token":"one","token":"two"}`, `{"token":42}`} {
		r := httptest.NewRequest("POST", "/api/v1/dans/session", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 || w.Body.String() != "{\"error\":\"unauthorized\"}\n" {
			t.Errorf("login rejection=%d %s", w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/dans/session", strings.NewReader("{"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("CSRF rejection=%d", w.Code)
	}
}

func TestBrowserSessionProxyRequiresConfiguredHandler(t *testing.T) {
	t.Parallel()
	proxy := &PowerDNSProxy{}
	for _, handler := range []http.HandlerFunc{proxy.CreateBrowserSession, proxy.DeleteBrowserSession} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/api/v1/dans/session", nil))
		if w.Code != 503 {
			t.Errorf("missing browser handler status=%d", w.Code)
		}
	}
	proxy.BrowserSessions = &BrowserSessions{}
	w := httptest.NewRecorder()
	proxy.DeleteBrowserSession(w, httptest.NewRequest("DELETE", "/api/v1/dans/session", nil))
	if w.Code != 204 || len(w.Result().Cookies()) != 1 {
		t.Errorf("browser handler was not called: %d", w.Code)
	}
}
