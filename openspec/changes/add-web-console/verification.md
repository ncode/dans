# Implementation verification

This note records synthetic local verification; raw captures, service credentials, connection configuration, and operational logs are deliberately excluded.

## Guidance and baseline

Loaded the OpenSpec apply workflow; frontend design foundations, create mode, accessibility, and rendered-verification guidance; Go HTTP, concurrency, errors, testing, tooling, and performance guidance; test-driven development and testing anti-patterns; surgical-change guidelines; and isolated-worktree guidance. The unchanged baseline passed `go test ./...` before implementation.

After integration into the working checkout, `GOTOOLCHAIN=go1.26.5 go test ./...`, all eleven frontend unit checks, TypeScript checking, the production frontend build, strict OpenSpec validation, and `git diff --check` pass. The rebuilt embedded assets match the verified feature-worktree assets byte-for-byte.

The final generation check found toolchain-dependent compression bytes in the embedded API specification. The decoded contract and all other generated code were identical. Generation is now pinned to Go 1.26.5, matching CI/releases, and the compressed artifact was refreshed with that toolchain.

## Safari HTTP development correction

The original browser verification covered Chromium and missed Safari's rejection of Secure cookies on the observed loopback HTTP origin. A native Safari cookie probe reproduced the distinction: the Secure cookie was dropped while an otherwise equivalent plain cookie was returned. The API independently accepted the existing token and issued a session, isolating the failure to cookie transport.

The local Compose stack now explicitly uses `development-http` cookie mode. Regression checks cover HTTP login/logout, cookie attributes, CSRF rejection, ambiguous/duplicate credentials, secure-default rejection of the development cookie, and current-authority revalidation on delegated writes in both modes. CLI checks cover the secure default, explicit development mode, flag precedence, and invalid-mode rejection. Go 1.26.5 `go test -race ./...`, `go vet ./...`, the development contract, and strict OpenSpec validation pass. The rebuilt local service also passes a real HTTP cookie-jar check for login, authenticated reads, cross-origin rejection, logout, and original-token reuse.

The user confirmed successful native Safari sign-in over the local HTTP URL after the correction. The full capacity and canonical release measurements below predate this cookie-mode correction; their hashes identify the original verified artifacts, not the corrected development image. Frontend assets are unchanged.

## Automated behavior and contracts

- `go test -race ./...`, `go vet ./...`, and `make generate-check` pass. Pinned Go 1.26.5 also passes the API, CLI, and static-route packages; the full real-system suites use that release toolchain.
- Both canonical `scripts/integration.sh` runs pass on PostgreSQL 16 and 18, including fault injection and recovery, audit persistence failures, schema mismatch, and deletion/recreation/rebinding. Complete database and HTTP integration suites also pass with `-race -tags integration` on PostgreSQL 16 and 18, including current-authority/session validation, expiry/revocation, restore finalization, schema mismatch, TLS/cookie/CSRF behavior, generation/cursor fencing, full/incremental refresh races, interrupted claims, and lifecycle invalidation.
- Frontend unit checks cover complete RRset comparison, preservation of disabled values/comments, single-grant authorization, root and reverse zones, literal wildcard/apex restrictions, valid hyphenated types, bounded wildcard matching, unknown writes without retry, and successful statuses with incomplete optional bodies. TypeScript and production bundling pass.
- Rendered Chromium tests cover preserved edit drafts/conflicts and creation collisions, mid-edit denial, uncertain writes, cancellation, operator/delegated navigation, failed sign-out clearing protected content, paginated server filters, indexing/empty/stale states, keyboard skip/focus restoration, long values, and narrow layout.
- Native 200% Chromium zoom uses a fresh profile with the browser's page-zoom preference. The test asserts a 640-CSS-pixel viewport and device scale 2 inside a 1280-pixel window, then exercises forms and focus. This is not CSS transform scaling. Captures are inspected locally and excluded from the repository.

## Actual authoritative API capacity

The fixture uses 400 synthetic zones, including 250,000 explicitly seeded RRsets plus the large zone's SOA and NS RRsets. It was seeded exclusively through real PowerDNS 5.1.3 HTTP API calls; DANS never accesses the PowerDNS database. Two complete application processes share PostgreSQL 16 and use the normal middleware, runtime grants, streaming worker, and browse endpoints.

| Measurement | Result |
| --- | ---: |
| PowerDNS API fixture seeding | 379.658 s |
| Cold large-zone indexing | 34.109 s |
| Complete traversal | 250,002 RRsets, 2,501 pages |
| Warm filtered 100-row page | 110.220 ms |
| Largest observed browse response | 17,309 bytes |
| Cross-instance REPLACE visibility | 152.316 ms |
| Cross-instance DELETE visibility | 101.493 ms |
| Cross-instance restore-write visibility | 148.859 ms |
| Full capacity verification after seeding | 98.339 s |
| Sampled maximum application RSS, instance A / B | 39,632 / 35,104 KiB |

These are observed local workload measurements, not universal latency guarantees. RSS was sampled, not a proof of an absolute memory maximum. The warm response never contains the whole large zone. Traversal verifies deterministic name/type order, uniqueness, complete values and disabled state; value order is compared semantically because PowerDNS may reorder it. The five-second target passes under these healthy dependency conditions.

Reproduce with `scripts/browse-capacity.py` after provisioning two matching application instances and an isolated compatible PowerDNS service. Supply `DANS_CAPACITY_PRIMARY_URL`, `DANS_CAPACITY_SECONDARY_URL`, `DANS_CAPACITY_TOKEN_FILE`, `DANS_CAPACITY_UPSTREAM_URL`, and `DANS_CAPACITY_UPSTREAM_KEY_FILE` from private environment/configuration. Defaults are `--zones 400 --rrsets 250000`; `--skip-seed` reuses the synthetic fixture. The harness never retries writes.

A separate earlier streaming-source test exercised the production collector/store against a synthetic HTTP upstream. Its results are not substituted for the real PowerDNS measurements above.

## Actual lifecycle verification

`scripts/browse-capacity.py --lifecycle-only` passes against both real application instances: delegated write and indexed read; zone deletion retires the binding and revokes the grant; PowerDNS returns 404; same-name recreation creates a fresh binding; the old delegated token gets 403; and rebuilt browsing contains no old RRset. The temporary zone is removed and the test token/identity invalidated.

## Live embedded browser

The opt-in `web/tests/live.spec.ts` passes against real authoritative DNS and both application instances. It verifies login and cookie attributes, remembered sessions after reload and across instances, complete multi-value RRset creation/edit/delete with a disabled value and comment preserved, unrestricted delegation creation/inspection/revocation, audit inspection, zone creation/edit/delete, and sign-out. All browser network requests stay on the explicitly configured application origins. `CONSOLE_LIVE_URL`, optional `CONSOLE_LIVE_SECONDARY_URL`, and `CONSOLE_LIVE_TOKEN_FILE` select a private isolated fixture; run after the capacity seed creates the reserved small synthetic zone.

All nine deterministic rendered regressions pass, together with eleven frontend unit checks. The optional live case is skipped without explicit live-fixture configuration. Desktop Chromium, native 200% Chromium zoom, and a 320-pixel viewport were exercised. Safari sign-in over local HTTP was subsequently verified by the user; the full workflow suite has not been run in other browser engines. Development-mode Cloudscape/React emits a child-key warning and a select-button attribute warning; the tested production workflows complete successfully. The production build reports a large-chunk advisory; no threshold was raised to hide it.

## Canonical runtime and release checks

The existing full integration harness ran unchanged inside a temporary Linux arm64 helper in the local Docker VM. This allowed its `/proc` VmRSS and startup measurements to use real Linux process data. Both PostgreSQL versions completed their fault/lifecycle workflows successfully.

| Runtime measurement | PostgreSQL 16 | PostgreSQL 18 |
| --- | ---: | ---: |
| Successful ready-start samples | 15 | 15 |
| Readiness median | 1.46 s | 0.96 s |
| Readiness p95 / maximum | 2.29 s | 1.23 s |
| Maximum of five ready-idle VmRSS samples | 28,088 KiB | 25,936 KiB |

Both runs retain the existing five-second readiness and 65,536-KiB ready-idle memory limits. Deployment smoke and release/workflow/footprint/development/integration contract checks pass. Actual container checks return 200 for `/console/` and its local referenced assets and 404 for an unknown API route.

The original implementation's release gates passed with Node.js 24.21.0 and Go 1.26.5. Both architectures were byte-identical across two builds; checksums, target architecture, corruption rejection, oversized-artifact rejection, container user/version/command surfaces, and embedded static delivery passed. These measurements predate the Safari cookie-mode correction. The runtime image contains the executable and CA certificates, without Node.js.

| Original verified artifact | amd64 | arm64 | Existing limit |
| --- | ---: | ---: | ---: |
| Executable | 22,954,110 bytes | 21,495,934 bytes | 25,165,824 bytes |
| OCI root filesystem | 23,203,840 bytes | 21,745,664 bytes | 27,262,976 bytes |

These artifacts embed the final frontend asset set. The original runtime startup/RSS/fault measurements preceded the last frontend error-state correction, and executable/OCI gates were repeated afterward. The subsequent Safari cookie-mode correction changed Go/runtime configuration and is covered by the separately recorded regression checks above. Across additional initial instance samples, the largest observed idle RSS was 28,712 KiB, also below the existing limit. Sanitized toolchains, measurements, original artifact hashes, and exact checks are retained in [evidence/release.json](evidence/release.json).

Strict OpenSpec validation and changed-file privacy/scope checks pass. All requested tasks are complete. Raw operational evidence and captures remain excluded from version control.
