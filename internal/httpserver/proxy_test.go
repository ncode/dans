package httpserver

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
	"github.com/ncode/dans/internal/upstream"
)

func TestPowerDNSProxyForwardsGeneratedRequestsAndRelaysRawResponses(t *testing.T) {
	t.Parallel()

	requests := make(chan *http.Request, 2)
	bodies := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		requests <- request.Clone(request.Context())
		bodies <- string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-PowerDNS-Extension", "preserved")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"upstream_extension":true}`)
	}))
	t.Cleanup(server.Close)

	client, err := upstream.New(
		upstream.TransportConfig{URL: server.URL, Timeout: time.Second},
		httpapi.NewSecret("upstream-key"),
	)
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	mutations := &recordingMutationStore{decision: database.AuthorizationDecision{
		Allowed: true,
		Actor: database.Actor{
			IdentityID: "00000000-0000-4000-8000-000000000001",
			TokenID:    "00000000-0000-4000-8000-000000000002",
			RequestID:  "request-patch",
		},
		ZoneBindingID:        "00000000-0000-4000-8000-000000000003",
		MatchedDelegationIDs: []string{"00000000-0000-4000-8000-000000000004"},
	}}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: mutations, UpstreamID: "primary", MutationTimeout: time.Second}

	zone := "example."
	readRequest := httptest.NewRequest(http.MethodGet, "/api/v1/servers/localhost/zones?zone=example.", nil)
	readRequest = readRequest.WithContext(WithRequestID(readRequest.Context(), "request-read"))
	readResponse := httptest.NewRecorder()
	proxy.ListZones(readResponse, readRequest, api.ServerId("localhost"), api.ListZonesParams{Zone: &zone})

	assertRelayedResponse(t, readResponse, "request-read")
	readUpstream := <-requests
	if readUpstream.Method != http.MethodGet || readUpstream.URL.EscapedPath() != "/api/v1/servers/localhost/zones" || readUpstream.URL.Query().Get("zone") != zone {
		t.Errorf("read upstream request = %s %s", readUpstream.Method, readUpstream.URL.String())
	}
	if got := readUpstream.Header.Get("X-API-Key"); got != "upstream-key" {
		t.Errorf("upstream key = %q", got)
	}
	if body := <-bodies; body != "" {
		t.Errorf("read body = %q", body)
	}

	patchBody := `{"rrsets":[{"name":"www.example.","type":"A","changetype":"REPLACE","ttl":300,"records":[{"content":"192.0.2.1","disabled":false}]}]}`
	patchRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/localhost/zones/example.", strings.NewReader(patchBody))
	patchRequest.Header.Set("Content-Type", "application/json")
	patchRequest.Header.Set("X-API-Key", validToken())
	patchRequest = patchRequest.WithContext(WithRequestID(patchRequest.Context(), "request-patch"))
	patchResponse := httptest.NewRecorder()
	proxy.PatchZone(patchResponse, patchRequest, api.ServerId("localhost"), api.ZoneId("example."))

	assertRelayedResponse(t, patchResponse, "request-patch")
	patchUpstream := <-requests
	if patchUpstream.Method != http.MethodPatch || patchUpstream.URL.EscapedPath() != "/api/v1/servers/localhost/zones/example." {
		t.Errorf("patch upstream request = %s %s", patchUpstream.Method, patchUpstream.URL.String())
	}
	if body := <-bodies; body != patchBody {
		t.Errorf("patch body = %q, want %q", body, patchBody)
	}
	intent, outcomes := mutations.snapshot()
	if len(intent) != 1 || len(outcomes) != 1 {
		t.Fatalf("audit writes = %d intents, %d outcomes", len(intent), len(outcomes))
	}
	if got, want := intent[0].RequestDigest, sha256.Sum256([]byte(patchBody)); string(got) != string(want[:]) {
		t.Errorf("request digest = %x, want %x", got, want)
	}
	if outcomes[0].Result != "succeeded" || outcomes[0].ResponseCode == nil || *outcomes[0].ResponseCode != http.StatusAccepted {
		t.Errorf("outcome = %+v", outcomes[0])
	}
}

func TestPowerDNSProxyRejectsWholePatchWhenAnyRRsetIsDenied(t *testing.T) {
	t.Parallel()

	var upstreamCalls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls++ }))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	mutations := &recordingMutationStore{decision: database.AuthorizationDecision{
		Allowed: false,
		Denied: []database.DeniedRRsetTuple{
			{Index: 0, Owner: "ok.example.", RecordType: "A", ChangeKind: "REPLACE"},
			{Index: 1, Owner: "blocked.example.", RecordType: "PTR", ChangeKind: "DELETE"},
		},
	}}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: mutations, UpstreamID: "primary", MutationTimeout: time.Second}
	body := `{"rrsets":[{"name":"ok.example.","type":"A","changetype":"REPLACE","ttl":300,"records":[]},{"name":"blocked.example.","type":"PTR","changetype":"DELETE"}]}`
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/localhost/zones/example.", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", validToken())
	request = request.WithContext(WithRequestID(request.Context(), "request-denied"))
	response := httptest.NewRecorder()

	proxy.PatchZone(response, request, api.ServerId("localhost"), api.ZoneId("example."))

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
	if upstreamCalls != 0 {
		t.Errorf("upstream calls = %d, want 0", upstreamCalls)
	}
	if !strings.Contains(response.Body.String(), "blocked.example.") || strings.Contains(response.Body.String(), "delegation") {
		t.Errorf("denial body does not contain only safe tuple details: %s", response.Body.String())
	}
	intents, outcomes := mutations.snapshot()
	if len(intents) != 0 || len(outcomes) != 0 {
		t.Errorf("denied patch created mutation audit: intents=%d outcomes=%d", len(intents), len(outcomes))
	}
}

func TestPowerDNSProxyLatchesDelegatedDenialAuditFailure(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls.Add(1) }))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	failures := &recordingAuditFailureReporter{}
	proxy := &PowerDNSProxy{
		Upstream: client, Mutations: &recordingMutationStore{authorizeErr: database.ErrAuditUnavailable},
		AuditFailures: failures, UpstreamID: "primary", MutationTimeout: time.Second,
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/localhost/zones/example.", strings.NewReader(`{"rrsets":[{"name":"blocked.example.","type":"A","changetype":"DELETE"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", validToken())
	request = request.WithContext(WithRequestID(request.Context(), "request-denial-audit-failure"))
	response := httptest.NewRecorder()

	proxy.PatchZone(response, request, api.ServerId("localhost"), api.ZoneId("example."))

	if response.Code != http.StatusServiceUnavailable || upstreamCalls.Load() != 0 || failures.count() != 1 {
		t.Fatalf("response = %d, upstream calls = %d, failure reports = %d", response.Code, upstreamCalls.Load(), failures.count())
	}
}

func TestPowerDNSProxyDoesNotForwardWithoutDurableIntent(t *testing.T) {
	t.Parallel()

	var upstreamCalls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls++ }))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	mutations := &recordingMutationStore{
		decision:  database.AuthorizationDecision{Allowed: true, Actor: database.Actor{IdentityID: "actor", TokenID: "token", RequestID: "request-intent"}},
		intentErr: database.ErrAuditUnavailable,
	}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: mutations, UpstreamID: "primary", MutationTimeout: time.Second}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/localhost/zones/example.", strings.NewReader(`{"rrsets":[{"name":"www.example.","type":"A","changetype":"DELETE"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", validToken())
	request = request.WithContext(WithRequestID(request.Context(), "request-intent"))
	response := httptest.NewRecorder()

	proxy.PatchZone(response, request, api.ServerId("localhost"), api.ZoneId("example."))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if upstreamCalls != 0 {
		t.Errorf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func TestPowerDNSProxyPreservesObservedResponseWhenOutcomePersistenceFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-PowerDNS-Extension", "preserved")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"observed upstream response"}`)
	}))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	mutations := &recordingMutationStore{
		decision:   database.AuthorizationDecision{Allowed: true, Actor: database.Actor{IdentityID: "actor", TokenID: "token", RequestID: "request-outcome"}},
		outcomeErr: database.ErrAuditUnavailable,
	}
	auditHealth := &recordingAuditFailureReporter{}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: mutations, AuditFailures: auditHealth, UpstreamID: "primary", MutationTimeout: time.Second}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/localhost/zones/example.", strings.NewReader(`{"rrsets":[{"name":"www.example.","type":"A","changetype":"DELETE"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", validToken())
	request = request.WithContext(WithRequestID(request.Context(), "request-outcome"))
	response := httptest.NewRecorder()

	proxy.PatchZone(response, request, api.ServerId("localhost"), api.ZoneId("example."))

	if response.Code != http.StatusConflict || response.Body.String() != `{"error":"observed upstream response"}` {
		t.Errorf("observed response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("X-PowerDNS-Extension") != "preserved" {
		t.Errorf("observed response headers were replaced: %v", response.Header())
	}
	if auditHealth.count() != 1 {
		t.Errorf("audit failure reports = %d, want 1", auditHealth.count())
	}
}

func TestPowerDNSProxyAuditsOperatorMutationBeforeForwardWithoutRetainingSecret(t *testing.T) {
	t.Parallel()

	mutations := &recordingMutationStore{}
	var forwardedBeforeIntent bool
	var forwardedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		intents, _ := mutations.snapshot()
		forwardedBeforeIntent = len(intents) == 0
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read forwarded body: %v", err)
		}
		forwardedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: mutations, UpstreamID: "primary", MutationTimeout: time.Second}
	secretBody := `{"name":"transfer-key.","algorithm":"hmac-sha256","key":"do-not-audit-this-secret"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/localhost/tsigkeys", strings.NewReader(secretBody))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(WithRequestID(request.Context(), "request-tsig"), actorKey, database.Actor{
		IdentityID: "00000000-0000-4000-8000-000000000001",
		TokenID:    "00000000-0000-4000-8000-000000000002",
		Operator:   true,
		RequestID:  "request-tsig",
	}))
	response := httptest.NewRecorder()

	proxy.CreateTSIGKey(response, request, api.ServerId("localhost"))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if forwardedBeforeIntent {
		t.Error("upstream mutation started before its durable intent")
	}
	if forwardedBody != secretBody {
		t.Errorf("forwarded body = %q, want exact original", forwardedBody)
	}
	intents, outcomes := mutations.snapshot()
	if len(intents) != 1 || len(outcomes) != 1 {
		t.Fatalf("audit writes = %d intents, %d outcomes", len(intents), len(outcomes))
	}
	if intents[0].Action != "powerdns.tsig_key.create" || intents[0].TargetID != "primary:/api/v1/servers/localhost/tsigkeys" {
		t.Errorf("intent classification = %+v", intents[0])
	}
	if strings.Contains(fmt.Sprintf("%+v", intents[0]), "do-not-audit-this-secret") {
		t.Errorf("intent retained secret-bearing request body: %+v", intents[0])
	}
}

func TestPowerDNSProxyForwardsCorrectedPutBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		body string
		call func(*PowerDNSProxy, http.ResponseWriter, *http.Request)
	}{
		{
			name: "cryptokey",
			path: "/api/v1/servers/localhost/zones/example./cryptokeys/7",
			body: `{"active":true,"published":false}`,
			call: func(proxy *PowerDNSProxy, response http.ResponseWriter, request *http.Request) {
				proxy.ModifyCryptokey(response, request, api.ServerId("localhost"), api.ZoneId("example."), "7")
			},
		},
		{
			name: "metadata",
			path: "/api/v1/servers/localhost/zones/example./metadata/ALLOW-AXFR-FROM",
			body: `{"metadata":["192.0.2.0/24"]}`,
			call: func(proxy *PowerDNSProxy, response http.ResponseWriter, request *http.Request) {
				proxy.ModifyMetadata(response, request, api.ServerId("localhost"), api.ZoneId("example."), "ALLOW-AXFR-FROM")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			forwarded := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read forwarded body: %v", err)
				}
				forwarded <- string(body)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)
			client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
			if err != nil {
				t.Fatalf("construct upstream client: %v", err)
			}
			proxy := &PowerDNSProxy{
				Upstream: client, Mutations: &recordingMutationStore{},
				UpstreamID: "primary", MutationTimeout: time.Second,
			}
			request := httptest.NewRequest(http.MethodPut, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request = request.WithContext(context.WithValue(WithRequestID(request.Context(), "request-put"), actorKey, database.Actor{
				IdentityID: "00000000-0000-4000-8000-000000000001",
				TokenID:    "00000000-0000-4000-8000-000000000002",
				Operator:   true,
			}))
			response := httptest.NewRecorder()

			test.call(proxy, response, request)

			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
			}
			if body := <-forwarded; body != test.body {
				t.Errorf("forwarded body = %q, want %q", body, test.body)
			}
		})
	}
}

func TestPowerDNSProxyCreatesBindingForObservedCreatedZone(t *testing.T) {
	t.Parallel()

	mutations := &recordingMutationStore{}
	lifecycle := &recordingZoneLifecycle{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"example.org.","name":"example.org.","upstream_extension":true}`)
	}))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	proxy := &PowerDNSProxy{
		Upstream: client, Mutations: mutations, Lifecycle: lifecycle,
		AuditFailures: &recordingAuditFailureReporter{}, LifecycleFailures: &recordingAuditFailureReporter{},
		UpstreamID: "primary", MutationTimeout: time.Second,
	}
	body := `{"name":"example.org.","kind":"Native","nameservers":["ns1.example.org."]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/localhost/zones", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = operatorRequestContext(request, "request-zone-create")
	response := httptest.NewRecorder()

	proxy.CreateZone(response, request, api.ServerId("localhost"), api.CreateZoneParams{})

	if response.Code != http.StatusCreated || response.Body.String() != `{"id":"example.org.","name":"example.org.","upstream_extension":true}` {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	bindings, _, _ := lifecycle.snapshot()
	if len(bindings) != 1 || bindings[0].Upstream != "primary" || bindings[0].PowerDNSZoneID != "example.org." || bindings[0].ZoneName != "example.org." {
		t.Errorf("created bindings = %+v", bindings)
	}
	intents, outcomes := mutations.snapshot()
	if len(intents) != 1 || len(outcomes) != 1 || outcomes[0].Result != "succeeded" {
		t.Errorf("audit writes = intents=%+v outcomes=%+v", intents, outcomes)
	}
}

func TestPowerDNSProxyRecordsDefiniteMutationOutcomeBeforeSlowBodyRelay(t *testing.T) {
	t.Parallel()

	outcomeRecorded := make(chan struct{})
	var outcomeBeforeBody atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.(http.Flusher).Flush()
		select {
		case <-outcomeRecorded:
			outcomeBeforeBody.Store(true)
		case <-time.After(250 * time.Millisecond):
		}
		_, _ = io.WriteString(w, `{"slow":"body"}`)
	}))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	mutations := &recordingMutationStore{outcomeRecorded: outcomeRecorded}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: mutations, UpstreamID: "primary", MutationTimeout: time.Second}
	request := operatorRequestContext(httptest.NewRequest(http.MethodPut, "/api/v1/servers/localhost/zones/example.org.", strings.NewReader(`{"kind":"Native"}`)), "request-slow-body")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	proxy.PutZone(response, request, api.ServerId("localhost"), api.ZoneId("example.org."))

	if !outcomeBeforeBody.Load() {
		t.Fatal("definite mutation outcome was not recorded before the response body relay")
	}
	if response.Code != http.StatusAccepted || response.Body.String() != `{"slow":"body"}` {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestPowerDNSProxyRecordsDefiniteDeletionOutcomeBeforeSlowBodyRelay(t *testing.T) {
	t.Parallel()

	outcomeRecorded := make(chan struct{})
	var outcomeBeforeBody atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.(http.Flusher).Flush()
		select {
		case <-outcomeRecorded:
			outcomeBeforeBody.Store(true)
		case <-time.After(250 * time.Millisecond):
		}
		_, _ = io.WriteString(w, `{"slow":"delete"}`)
	}))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	lifecycle := &recordingZoneLifecycle{outcomeRecorded: outcomeRecorded}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: &recordingMutationStore{}, Lifecycle: lifecycle, UpstreamID: "primary", MutationTimeout: time.Second}
	request := operatorRequestContext(httptest.NewRequest(http.MethodDelete, "/api/v1/servers/localhost/zones/example.org.", nil), "request-slow-delete")
	response := httptest.NewRecorder()

	proxy.DeleteZone(response, request, api.ServerId("localhost"), api.ZoneId("example.org."))

	if !outcomeBeforeBody.Load() {
		t.Fatal("definite deletion outcome was not recorded before the response body relay")
	}
	if response.Code != http.StatusAccepted || response.Body.String() != `{"slow":"delete"}` {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestPowerDNSProxyPreservesCreatedZoneResponseAndLatchesBindingFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		response  string
		bindError error
	}{
		{name: "malformed response", response: `{"name":"example.org."}`},
		{name: "binding persistence failure", response: `{"id":"example.org.","name":"example.org."}`, bindError: database.ErrAuditUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, test.response)
			}))
			t.Cleanup(server.Close)
			client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
			if err != nil {
				t.Fatalf("construct upstream client: %v", err)
			}
			mutations := &recordingMutationStore{}
			lifecycle := &recordingZoneLifecycle{bindErr: test.bindError}
			failures := &recordingAuditFailureReporter{}
			proxy := &PowerDNSProxy{
				Upstream: client, Mutations: mutations, Lifecycle: lifecycle,
				AuditFailures: failures, LifecycleFailures: failures,
				UpstreamID: "primary", MutationTimeout: time.Second,
			}
			body := `{"name":"example.org.","kind":"Native"}`
			request := operatorRequestContext(httptest.NewRequest(http.MethodPost, "/api/v1/servers/localhost/zones", strings.NewReader(body)), "request-binding-failure")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			proxy.CreateZone(response, request, api.ServerId("localhost"), api.CreateZoneParams{})

			if response.Code != http.StatusCreated || response.Body.String() != test.response {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			if failures.lifecycleCount() != 1 {
				t.Fatalf("lifecycle failure reports = %d, want 1", failures.lifecycleCount())
			}
			_, outcomes := mutations.snapshot()
			if len(outcomes) != 1 || outcomes[0].Result != "unknown" || outcomes[0].ResponseClass != "binding_error" {
				t.Fatalf("outcomes = %+v", outcomes)
			}
		})
	}
}

func TestPowerDNSProxyRetiresAuthorityBeforeZoneDeleteAndTreatsAbsenceAsComplete(t *testing.T) {
	t.Parallel()

	lifecycle := &recordingZoneLifecycle{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, plans, _ := lifecycle.snapshot()
		if len(plans) == 0 {
			t.Error("zone deletion reached upstream before binding retirement and intent")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"zone absent"}`)
	}))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	proxy := &PowerDNSProxy{Upstream: client, Mutations: &recordingMutationStore{}, Lifecycle: lifecycle, UpstreamID: "primary", MutationTimeout: time.Second}
	request := operatorRequestContext(httptest.NewRequest(http.MethodDelete, "/api/v1/servers/localhost/zones/example.org.", nil), "request-zone-delete")
	response := httptest.NewRecorder()

	proxy.DeleteZone(response, request, api.ServerId("localhost"), api.ZoneId("example.org."))

	if response.Code != http.StatusNotFound || response.Body.String() != `{"error":"zone absent"}` {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	_, plans, outcomes := lifecycle.snapshot()
	if len(plans) != 1 || plans[0].Upstream != "primary" || plans[0].PowerDNSZoneID != "example.org." {
		t.Fatalf("deletion plans = %+v", plans)
	}
	if len(outcomes) != 1 || outcomes[0].Result != database.ZoneDeletionNotFound || outcomes[0].ResponseCode == nil || *outcomes[0].ResponseCode != http.StatusNotFound {
		t.Errorf("deletion outcomes = %+v", outcomes)
	}
}

func TestPowerDNSProxyMapsRetiredRawDeleteToConflictWithoutForwarding(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls.Add(1) }))
	t.Cleanup(server.Close)
	client, err := upstream.New(upstream.TransportConfig{URL: server.URL, Timeout: time.Second}, httpapi.NewSecret("upstream-key"))
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	proxy := &PowerDNSProxy{
		Upstream: client, Mutations: &recordingMutationStore{},
		Lifecycle:  &recordingZoneLifecycle{prepareErr: database.ErrConflict},
		UpstreamID: "primary", MutationTimeout: time.Second,
	}
	request := operatorRequestContext(httptest.NewRequest(http.MethodDelete, "/api/v1/servers/localhost/zones/retired.example.", nil), "request-retired-delete")
	response := httptest.NewRecorder()

	proxy.DeleteZone(response, request, api.ServerId("localhost"), api.ZoneId("retired.example."))

	if response.Code != http.StatusConflict || upstreamCalls.Load() != 0 {
		t.Fatalf("response = %d, upstream calls = %d", response.Code, upstreamCalls.Load())
	}
}

func TestPowerDNSProxyMapsTransportFailure(t *testing.T) {
	t.Parallel()

	client, err := upstream.New(
		upstream.TransportConfig{URL: "http://127.0.0.1:1", Timeout: 100 * time.Millisecond},
		httpapi.NewSecret("upstream-key"),
	)
	if err != nil {
		t.Fatalf("construct upstream client: %v", err)
	}
	proxy := &PowerDNSProxy{Upstream: client}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	request = request.WithContext(WithRequestID(request.Context(), "request-failure"))
	response := httptest.NewRecorder()

	proxy.ListServers(response, request)

	if response.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	if strings.Contains(response.Body.String(), "127.0.0.1") {
		t.Errorf("response leaks internal endpoint: %s", response.Body.String())
	}
}

func assertRelayedResponse(t *testing.T, response *httptest.ResponseRecorder, requestID string) {
	t.Helper()
	if response.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if response.Body.String() != `{"upstream_extension":true}` {
		t.Errorf("body = %q", response.Body.String())
	}
	if got := response.Header().Get("X-PowerDNS-Extension"); got != "preserved" {
		t.Errorf("upstream extension header = %q", got)
	}
	if got := response.Header().Get("X-Request-ID"); got != requestID {
		t.Errorf("request ID = %q, want %q", got, requestID)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache control = %q", got)
	}
}

var _ api.ServerInterface = (*PowerDNSProxy)(nil)

type recordingMutationStore struct {
	mu              sync.Mutex
	decision        database.AuthorizationDecision
	authorizeErr    error
	intentErr       error
	outcomeErr      error
	intents         []database.DNSIntentInput
	outcomes        []database.DNSOutcomeInput
	outcomeRecorded chan struct{}
	outcomeOnce     sync.Once
}

type recordingAuditFailureReporter struct {
	mu                sync.Mutex
	failures          int
	lifecycleFailures int
}

func (reporter *recordingAuditFailureReporter) ReportLifecycleFailure() {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	reporter.lifecycleFailures++
}

func (reporter *recordingAuditFailureReporter) ReportAuditFailure() {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	reporter.failures++
}

func (reporter *recordingAuditFailureReporter) count() int {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	return reporter.failures
}

func (reporter *recordingAuditFailureReporter) lifecycleCount() int {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	return reporter.lifecycleFailures
}

type recordingZoneLifecycle struct {
	mu              sync.Mutex
	bindErr         error
	prepareErr      error
	bindings        []database.ZoneBindingInput
	plans           []database.ZoneDeletionByUpstreamInput
	outcomes        []database.ZoneDeletionOutcomeInput
	outcomeRecorded chan struct{}
	outcomeOnce     sync.Once
}

func (lifecycle *recordingZoneLifecycle) BindCreatedZone(_ context.Context, _ database.Actor, input database.ZoneBindingInput) (database.ZoneBinding, error) {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	lifecycle.bindings = append(lifecycle.bindings, input)
	return database.ZoneBinding{}, lifecycle.bindErr
}

func (lifecycle *recordingZoneLifecycle) PrepareZoneDeletionByUpstreamZoneID(_ context.Context, _ database.Actor, input database.ZoneDeletionByUpstreamInput) (database.ZoneDeletionPlan, error) {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	lifecycle.plans = append(lifecycle.plans, input)
	if lifecycle.prepareErr != nil {
		return database.ZoneDeletionPlan{}, lifecycle.prepareErr
	}
	return database.ZoneDeletionPlan{
		Intent: database.DNSIntent{
			EventID:     "00000000-0000-4000-8000-000000000020",
			OperationID: "00000000-0000-4000-8000-000000000021",
			Deadline:    input.Deadline,
		},
		Upstream: input.Upstream, PowerDNSZoneID: input.PowerDNSZoneID,
	}, nil
}

func (lifecycle *recordingZoneLifecycle) RecordZoneDeletionOutcome(_ context.Context, _ database.ZoneDeletionPlan, input database.ZoneDeletionOutcomeInput) (database.DNSOutcome, error) {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	lifecycle.outcomes = append(lifecycle.outcomes, input)
	if lifecycle.outcomeRecorded != nil {
		lifecycle.outcomeOnce.Do(func() { close(lifecycle.outcomeRecorded) })
	}
	return database.DNSOutcome{}, nil
}

func (lifecycle *recordingZoneLifecycle) snapshot() ([]database.ZoneBindingInput, []database.ZoneDeletionByUpstreamInput, []database.ZoneDeletionOutcomeInput) {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return append([]database.ZoneBindingInput(nil), lifecycle.bindings...), append([]database.ZoneDeletionByUpstreamInput(nil), lifecycle.plans...), append([]database.ZoneDeletionOutcomeInput(nil), lifecycle.outcomes...)
}

func operatorRequestContext(request *http.Request, requestID string) *http.Request {
	return request.WithContext(context.WithValue(WithRequestID(request.Context(), requestID), actorKey, database.Actor{
		IdentityID: "00000000-0000-4000-8000-000000000001",
		TokenID:    "00000000-0000-4000-8000-000000000002",
		Operator:   true,
		RequestID:  requestID,
	}))
}

func (store *recordingMutationStore) AuthorizeRRsetBatch(context.Context, string, string, string, string, []database.RRsetTuple) (database.AuthorizationDecision, error) {
	return store.decision, store.authorizeErr
}

func (store *recordingMutationStore) CreateDNSIntent(_ context.Context, _ database.Actor, input database.DNSIntentInput) (database.DNSIntent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.intents = append(store.intents, input)
	if store.intentErr != nil {
		return database.DNSIntent{}, store.intentErr
	}
	return database.DNSIntent{
		EventID:     "00000000-0000-4000-8000-000000000010",
		OperationID: "00000000-0000-4000-8000-000000000011",
		Deadline:    input.Deadline,
	}, nil
}

func (store *recordingMutationStore) RecordDNSOutcome(_ context.Context, input database.DNSOutcomeInput) (database.DNSOutcome, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.outcomes = append(store.outcomes, input)
	if store.outcomeRecorded != nil {
		store.outcomeOnce.Do(func() { close(store.outcomeRecorded) })
	}
	return database.DNSOutcome{}, store.outcomeErr
}

func (store *recordingMutationStore) snapshot() ([]database.DNSIntentInput, []database.DNSOutcomeInput) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]database.DNSIntentInput(nil), store.intents...), append([]database.DNSOutcomeInput(nil), store.outcomes...)
}
