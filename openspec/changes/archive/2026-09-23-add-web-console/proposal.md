## Why

DNS editing and delegated administration currently require API or CLI workflows. The agreed web console should make those tasks accessible through a Route 53-inspired interface while supporting 400 zones and a zone with 250,000 RRsets without downloading that zone into the browser.

## What Changes

- Add a React, TypeScript, and Cloudscape console embedded in the existing executable, covering zone/RRset CRUD, delegation management, and operator audit viewing.
- Add seven-day token-backed browser sign-in with a secure HttpOnly cookie, CSRF protection, immediate token/identity revocation checks, and independent browser sign-out.
- Enable explicit HTTP development cookies in the local Compose stack for Safari, and print the console URL and existing operator sign-in token after successful startup; deployed services retain secure cookie defaults.
- Add an internal PostgreSQL browse index populated only through the PowerDNS API, with 100-RRset pages, exact/prefix owner-name search, type filtering, and stable name/type ordering.
- Build browse data lazily, refresh affected RRsets within a measured five-second target after successful mutations, and refresh viewed zones every five minutes or manually, exposing loading/stale/error states.
- Enrich effective self-delegations with zone information for advisory permission explanations; preserve server-side authorization and shared authenticated DNS reads.
- Read complete live RRsets for editing and before saving, preserve drafts on detected conflicts, confirm destructive actions, and never automatically retry an uncertain DNS write.

## Capabilities

### New Capabilities

- `web-console`: Embedded console delivery, DNS and delegated-administration workflows, accessible interaction states, and browser verification.
- `browser-authentication`: Token-backed browser sessions, seven-day retention, CSRF protection, revocation, sign-out, and multi-instance behavior.
- `rrset-browse-index`: API-populated browse data, bounded pagination/filtering, refresh coordination, lifecycle isolation, and capacity/freshness verification.

### Modified Capabilities

- `identity-access-management`: Accept the new browser credential alongside the existing header-token contract and expose zone identity in effective self-delegations.
- `powerdns-api-gateway`: Extend client credential isolation to browser authentication while preserving the pinned compatibility surface and upstream response fidelity.

## Impact

Affects the HTTP router/authentication boundary, OpenAPI overlay and generated clients, database schema/queries, mutation observation and runtime workers, static asset build/release pipeline, local development setup, and integration tests. Adds a frontend build toolchain and Cloudscape dependencies, but no runtime JavaScript server, IdP, external index service, or direct PowerDNS database connection. New migrations retain coordinated upgrades and existing runtime database-role boundaries. Existing CLI clients, authorization semantics, DNS availability, and last-successful-write behavior remain supported.

## Non-goals

Identity/group administration UI, IdP integration, record-value search, guaranteed compare-and-set DNS writes, unsupported external zone lifecycle changes, automatic mutation replay, and public deployment are outside this change. The detailed agreement is recorded in `docs/frontend.md` and the browser-authentication and browse-index ADRs.
