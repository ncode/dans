# Public API and Go client

DANS serves the pinned PowerDNS Authoritative 5.1 compatibility API and DANS management resources from one OpenAPI contract. The normal base URL is `https://dans.example.com/api/v1`; `/livez`, `/readyz`, and `/api/docs` are rooted at the ingress host.

## Contract and authentication

`GET /livez` and `GET /readyz` are anonymous and return only 200 or 503. Every other declared route requires exactly one `X-API-Key` header containing a DANS token. `GET /api/docs` returns the combined OpenAPI 3.1 document to an authenticated caller:

```sh
dans --endpoint https://dans.example.com/api/v1 \
  --api-token-file /run/secrets/dans-token docs >dans-openapi.json
```

The PowerDNS key is never a client credential. DANS replaces the caller's token with its configured upstream key only after validation and authorization. Authenticated responses include `Cache-Control: no-store`, and every response includes `X-Request-ID` for log/audit correlation.

DANS-originated errors use JSON with a required `error` string and optional `errors` array. A forwarded PowerDNS response retains the upstream status, body bytes, and end-to-end headers; only hop-by-hop headers are removed. Clients that require byte fidelity should consume the generated response's `Body` and `HTTPResponse` rather than decode and re-encode a modeled value.

Collections return `items` and nullable `next_cursor`, default to 100 entries, and accept at most 500. A cursor is bounded and tied to its collection and filters. Every page is a new authentication/authorization decision and does not promise a cross-page snapshot or total count.

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
