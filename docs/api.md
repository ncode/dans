# Public API and Go client

DANS serves the pinned PowerDNS Authoritative 5.1 compatibility API and DANS management resources from one OpenAPI contract. The normal base URL is `https://dans.example.com/api/v1`; `/livez`, `/readyz`, and `/api/docs` are rooted at the ingress host.

## Contract and authentication

`GET /livez` and `GET /readyz` are anonymous and return only 200 or 503. The console shell and assets under `/console/` are public. Except for the explicit browser-session endpoints below, declared API routes require exactly one DANS credential: an `X-API-Key` header or the protected browser-session cookie. Supplying both kinds or duplicate credentials is rejected. `GET /api/docs` returns the combined OpenAPI 3.1 document to an authenticated caller:

```sh
dans --endpoint https://dans.example.com/api/v1 \
  --api-token-file /run/secrets/dans-token docs >dans-openapi.json
```

The PowerDNS key is never a client credential. DANS replaces the caller's token with its configured upstream key only after validation and authorization. Authenticated responses include `Cache-Control: no-store`, and every API response includes `X-Request-ID` for log/audit correlation.

DANS-originated errors use JSON with a required `error` string and optional `errors` array. A forwarded PowerDNS response retains the upstream status, body bytes, and end-to-end headers; only hop-by-hop headers are removed. Clients that require byte fidelity should consume the generated response's `Body` and `HTTPResponse` rather than decode and re-encode a modeled value.

Management collections return `items` and nullable `next_cursor`, default to 100 entries, and accept at most 500. A cursor is bounded and tied to its collection and filters. Every page is a new authentication/authorization decision and does not promise a cross-page snapshot or total count.

## Generated Go client

The importable generated package is:

```text
github.com/ncode/dans/api
```

It contains combined PowerDNS/DANS models, the generated client, and `DANSClientWithResponses`. The DANS wrapper keeps normal operations on `/api/v1` and routes document and health operations to their ingress-root paths. It defaults to a 30-second HTTP timeout and limits response bodies to 64 MiB; `api.WithHTTPClient` can replace the transport or timeout without bypassing the response limit. Add a request editor for the DANS credential:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ncode/dans/api"
)

func run(ctx context.Context) error {
	token := os.Getenv("DANS_API_TOKEN")
	if token == "" {
		return fmt.Errorf("token environment variable is required")
	}
	client, err := api.NewDANSClientWithResponses(
		"https://dans.example.com/api/v1",
		api.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			request.Header.Set("X-API-Key", token)
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("configure DANS client: %w", err)
	}

	response, err := client.GetCurrentIdentityWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("get current identity: %w", err)
	}
	if response.HTTPResponse == nil || response.HTTPResponse.StatusCode != http.StatusOK || response.JSON200 == nil {
		return fmt.Errorf("get current identity: %s", response.Status())
	}
	fmt.Println(response.JSON200.Handle)

	document, err := client.GetAPIDocumentWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("get combined API document: %w", err)
	}
	if document.HTTPResponse == nil || document.HTTPResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("get combined API document: %s", document.Status())
	}
	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
```

The snippet uses an environment token for brevity. Production callers should read an approved environment or mounted-file secret once, reject an empty value, and never log it. Do not use `http.DefaultClient`, add automatic mutation retries, or contact PowerDNS directly.

The checked-in client is generated deterministically from [`api/openapi/openapi.json`](../api/openapi/openapi.json). Compatibility is limited to the contract in that release; use the client and contract from the same DANS release.

## Authorization surface

PowerDNS zone list/get/export and search-data are shared authenticated reads. Zone `PATCH` is the only delegated compatibility write. Every other PowerDNS operation is operator-only. DANS `/me` routes enforce self ownership; other management and audit routes are operator-only.

See [policy.md](policy.md) for selector examples, PTR/apex/literal-wildcard behavior, route classes, and v1 limitations. See [cli.md](cli.md) for every supported command/configuration workflow. Rare pinned PowerDNS operations without a dedicated CLI command remain available through this generated client; DANS intentionally has no generic raw-request CLI command.

## Browser sessions

`POST /api/v1/dans/session` accepts one strict JSON object, `{"token":"<existing-api-token>"}`. A successful response is `201` with `expires_at`; the session secret is sent only in a `__Host-dans_session` cookie (`Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`, no Domain). The database stores a digest linked to the original token. Sessions expire after at most 168 hours, never renew automatically, and stop working as soon as the original token expires or is revoked or its identity is disabled. Every protected request rechecks current authority. JavaScript does not persist the token in localStorage or sessionStorage.

`DELETE /api/v1/dans/session` invalidates the current browser session and clears its cookie without revoking the original API token. It also clears malformed or expired cookies. A dependency failure still clears the browser cookie but returns `503`; it does not confirm durable invalidation. Revoke the original token when all sessions must end immediately. Signing in again intentionally replaces the current browser session.

Both endpoints enforce same-origin browser mutation protection. Production ingress must use HTTPS. Host-scoped cookies avoid accepting a credential from another host; cross-origin unsafe requests are rejected using browser fetch metadata and Origin checks. Non-browser header-token clients continue to use the existing API. Session endpoints never return reusable credentials in response JSON.

Local development can explicitly select `browser_cookie_mode=development-http`, as the root Compose stack does. This changes the cookie to `dans_dev_session` without Secure, allowing Safari to use the HTTP development URL. HttpOnly, SameSite=Strict, the host-only scope, session expiry, and all authentication/CSRF checks stay in place. The default secure mode does not accept the development cookie name.

## Indexed RRset browsing

`GET /api/v1/dans/servers/{server_id}/zones/{zone_id}/rrsets` returns up to 100 complete RRsets sorted by canonical owner name and type. It accepts `name`, `match=exact|prefix`, `type`, and `cursor`. Empty filters browse the whole zone by pages; values are not searched. Responses contain `items`, `next_cursor`, `state`, `last_refreshed_at`, and a nullable sanitized `error`.

First access returns `202` with state `indexing` while a background worker reads PowerDNS through its API. A complete generation returns `200`. During later refreshes the previous complete generation remains available with `refreshing` or `stale` state. `POST` to the same path plus `/refresh` schedules a full read and returns `202`; it does not mutate DNS. These routes are shared authenticated reads, independent of delegated write authority.

A browse cursor is bound to the zone, filters, lifetime, and content revision. It must be used with the original filters. Invalid cursors return `400`; a superseded generation or revision returns `409` and the client restarts at the first page. Unlike management collections, this endpoint has a fixed 100-row page size. Normal PowerDNS compatibility responses remain unchanged.

Affected RRsets are re-read after DANS mutations across API instances. Viewed zones also refresh fully every five minutes, and closed pages cease keeping zones active. The index is rebuildable read state; live PowerDNS reads and current authorization remain required for edits. Effective `/dans/me/delegations` entries additionally include `zone_id` and `zone_name` for permission explanations.
