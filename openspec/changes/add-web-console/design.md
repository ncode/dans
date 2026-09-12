## Context

See `proposal.md` for motivation, `docs/frontend.md` for the agreed product design, and ADRs 0011–0012 for the authentication and indexing decisions. The repository is a Go 1.26 service with generated OpenAPI/server/client types, pgx/sqlc persistence, a contract-validating HTTP gateway, and coordinated multi-instance deployments. It has no frontend or browser session support. Current zone reads can return a whole zone, and global search returns capped individual records without continuation.

## Goals / Non-Goals

**Goals:** deliver the complete agreed console through the existing executable; keep PowerDNS access API-only; bound large-zone browsing and background work; preserve the current authorization decision point and fail-closed behavior; use existing database, audit, generation, and test patterns.

**Non-Goals:** new DNS write semantics, an authority cache, direct PowerDNS database access, an external index/search service, IdP integration, a separate production Node server, or changes to unrelated benchmark work. The performance budgets already in the repository remain checks to run, not limits to silently raise.

## Decisions

### Frontend and delivery

Use React/TypeScript with Cloudscape's existing layout, table, form, dialog, and status primitives. Use direct component imports and local bundled styles/assets; avoid a custom design system or decorative dashboard. Keep navigation centered on Zones, Delegations, and Audit with role-aware actions, breadcrumbs, an RRset table, explicit create/edit forms, and persistent feedback. Identity/group resources are read for selector labels but not administered in this UI.

Use a small static frontend build (Vite is the default) with pinned dependency lockfile and scripts for type checking, component tests, and production build. Embed the built assets in Go. Update canonical local, Docker, CI, and release build entry points so clean builds include the UI and require no Node process at runtime. Keep generated asset handling explicit and reproducible; do not commit node_modules or a fallback placeholder UI.

Serve the public static shell at explicit console routes, outside API authentication/contract validation. Protect API paths with the existing middleware chain. Limit any SPA fallback to console paths: an undeclared `/api` request must remain an API rejection. Use same-origin requests; no CORS deployment or configurable arbitrary API endpoint is required.

### Opaque browser sessions in the existing database

Add browser sessions linked to the original API-token row. Generate a cryptographically random session secret, store only its digest with the original token ID and absolute expiry, and set a host-scoped Secure/HttpOnly/SameSite cookie. Server-side expiry is at most seven days and current original-token expiry/revocation and identity disablement always apply. This avoids retaining a recoverable original API token or adding a shared signing-key configuration. Sign-out invalidates only the current session and clears its cookie; expired credentials must still be clearable. Old/expired session rows need bounded cleanup.

Add explicit sign-in/sign-out operations to the contract. Sign-in validates one supplied API token and replaces any existing browser session intentionally; other protected requests reject ambiguous credential kinds or duplicates. Login responses expose non-secret session metadata only. Use Go's standard-library cross-origin protection for state-changing browser operations, plus the cookie attributes; keep it compatible with the documented TLS ingress and loopback-only development workflow without weakening production cookies.

The local Compose stack explicitly sets `browser_cookie_mode=development-http` to support Safari over HTTP. This uses the separate `dans_dev_session` cookie without Secure while retaining HttpOnly, SameSite=Strict, CSRF protection, and all session/authority checks. The default `secure` mode continues using `__Host-dans_session` and never accepts the development cookie name. Server configuration selects the mode; client Host/forwarding headers do not.

Resolve header and browser credentials into an internal authenticated credential representation. Browser credentials must participate in current session/token/identity checks at the applicable authorization decision point, including the delegated-write SQL path that currently re-reads the plaintext header token. Do not merely attach an Actor and bypass later credential revalidation. Extend the existing narrow authorization seam to accept a server-derived token/session digest or equivalent; never inject an invented plaintext API token. Keep current policy checks per request and never hold a database transaction across an upstream call.

An opaque database session was selected over localStorage/sessionStorage (script-readable reusable credentials), a raw API-token cookie with client-only expiry (does not enforce the session lifetime server-side), or an encrypted/signed envelope (adds shared key configuration). Current CLI header-token behavior remains available.

### Read index and bounded browsing API

Add application-owned browse operations under the management API using the same server/zone identifiers as the compatibility API. A browse operation accepts exact or prefix owner-name matching, optional record type, and a bounded opaque continuation cursor. Return up to 100 full RRsets, a nullable next cursor, and sanitized readiness/freshness metadata. Report initial indexing as accepted/in progress rather than empty data; use a normal page when a complete generation exists. A manual refresh operation schedules read work and does not modify DNS. The frontend keeps previous cursor positions for Previous/Next navigation.

Store browse-zone state, generation/version information, canonical owner/type keys, and complete RRset payloads in PostgreSQL. Index columns used for zone/name/type keyset selection; escape SQL wildcard characters when implementing prefix search and use deterministic bytewise canonical-name ordering. Do not apply filters after fetching a page or scan/deserialize the whole zone in the browser. Keep primary-key ordering and cursor validation separate from the existing creation-time/UUID management cursors, while reusing small encoding/validation helpers where appropriate.

Cursors carry zone, filters, and generation information and grant no authority. They must either traverse a retained consistent generation or fail explicitly when that generation can no longer be continued; do not silently mix snapshots and produce omissions. Handle zones without a delegation binding as readable DNS zones, and independently fence index lifetime changes so creating an index does not create delegated authority.

### Refresh collection and coordination

Build on first access, not at service startup. Coordinate claims through PostgreSQL using bounded leases/fencing and the repository's worker patterns, with recoverable claims after worker exit. Use one coherent owner/version scheme for full refreshes, incremental work, publication, and invalidation. Do not add a queue product, per-request goroutine, or a transaction held across HTTP calls.

Collect a full zone through the existing authenticated PowerDNS transport, streaming RRset decoding into bounded database batches for a staged generation. Consume and validate the complete response before atomically publishing it. Failed, oversized, truncated, or timed-out reads leave the prior complete generation intact and report stale/error state. Give background reads their own bounded cancellation/deadline and response safeguards; do not assume the ordinary generated client's 64 MiB decoding limit or its default request deadline can ingest every capacity fixture, and do not remove limits globally.

Record affected owner/type keys durably in the shared mutation-intent/outcome paths so browser, CLI, generated-client, and all-instance writes have the same invalidation behavior. Fetch the current complete affected RRsets after successful writes, including DELETE/EXTEND/PRUNE, rather than reconstructing values from an audit digest or applying request payloads optimistically. Unknown outcomes require read reconciliation, never mutation replay. Generic zone-affecting operations and lifecycle changes schedule the appropriate full refresh or invalidation.

Fence publication against mutations observed while a full snapshot is collected: retain/version pending changes and re-read them before or after publication so clearing work cannot discard a newer observation. A delete or retire invalidates prior generations and in-flight publishers before they can make an old lifetime visible again. A recreate/rebind starts new read state without inheriting grants. Existing unsupported external lifecycle behavior remains unsupported.

Successful writes target index visibility within five seconds under healthy dependencies at the agreed capacity. Zones being viewed refresh fully every five minutes, with shared deduplication and manual refresh support. Define activity through recent browse requests so closed pages do not keep all zones permanently refreshing. Expose last successful indexing time and a clear refresh state, not internal lease/connection diagnostics. Full refresh failures keep already displayed data visibly stale; current authorization and readiness failures still fail protected operations.

### Permission display and editing

Extend the effective-self-delegation query's existing binding join to project zone ID/name into an appropriate self-delegation response model. Do not make new required fields break unrelated delegation responses. Preserve pagination and active-lifetime filtering.

Use these grants only for advisory control state and explanations. Match a complete single grant for a tuple, then union matching grants; never combine a type restriction from one grant with an owner restriction from another. Include exact apex/literal-wildcard behavior. Unit tests must exercise these corner cases against representative backend expectations.

Open editors from fresh complete PowerDNS RRsets. Preserve multi-value content, comments/disabled state, and relevant fields when replacing an RRset. Before saving or deleting, read again and compare canonical semantic content with the editor baseline; on mismatch show live versus draft and require reconciliation. Creation checks for a newly existing RRset rather than silently replacing it. The check is advisory and retains the narrow check/write race; do not introduce distributed write locks or claim compare-and-set safety.

Every mutation uses the existing public API. Keep drafts on failures, show unknown outcomes distinctly, and disable duplicate submission while a request is pending. Never retry writes through a generic retry wrapper. Confirm destructive actions, including revoking a delegation and deleting a zone, with accessible dialogs and useful focus restoration.

### Validation and evidence

Use test-first changes at auth, cursor, refresh/publication, and editing boundaries. Reuse real PostgreSQL/PowerDNS integration infrastructure and current generation checks; add only the frontend checks needed for the new surface. Validate two instances, original-token/session revocation, CSRF, grant changes, initial/failed refreshes, full/incremental races, interrupted workers, and zone deletion/recreation.

Generate synthetic fixtures for 400 zones including 250,000 RRsets. Exercise the actual API-to-index ingestion path, indexed page/filter requests, complete values, absence of whole-zone browser downloads, and the five-second cross-instance refresh target. Record cold build time, warm page/filter latency, resource use, and mutation-to-index delay without substituting a small fixture or direct index seeding for ingestion evidence. Maintain the existing artifact/idle/startup gates; investigate failures rather than changing their thresholds silently.

Use a real rendered browser for sign-in, browse, edit, delegation, audit, failure, keyboard/focus, narrow layout, and zoom checks. Keep captures and raw operational logs private; report only sanitized synthetic evidence. Never claim runtime/accessibility/capacity checks from source inspection alone.

## Risks / Trade-offs

- Whole-zone refresh remains O(zone size) upstream → coalesce requests, run it outside browser requests, stream bounded batches, and measure the capacity fixture.
- Indexed browsing can lag or fail → expose freshness, preserve complete prior data, and require live reads for editing.
- A full refresh can race newer writes or a new zone lifetime → fence publication and preserve pending work across generation changes.
- Browser sessions extend the credential boundary → use opaque digests, current database validation, CSRF protection, explicit route/security classification, and cross-instance tests.
- Whole-RRset editing still has a check/write race → preserve drafts and explain detected changes without promising atomic conflict prevention.
- Embedded UI assets increase artifact size → use direct Cloudscape imports and production bundling, measure canonical releases, and keep Node out of runtime images.

## Migration Plan

1. Add forward-only session/index migrations, generated queries, and required runtime-role grants through the existing offline migration path. Do not migrate on API startup.
2. Build and verify complete assets, generated API/SQL outputs, and both supported release architectures.
3. Follow the existing coordinated-upgrade procedure: backup, drain all old instances, migrate once, then start the new same-version instances.
4. Start with no eager zone indexing; the first browse creates its read generation. Existing API tokens and CLI clients continue working.
5. Retain the existing restore-based rollback procedure for incompatible migrations. Read index data is rebuildable; restored token invalidation must also make restored browser sessions unusable.
