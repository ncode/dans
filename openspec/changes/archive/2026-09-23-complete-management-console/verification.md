# Management console verification

Verification date: 2026-09-12. All datasets and credentials were synthetic and isolated from the development stack. Raw logs, credentials, screenshots, and machine details are retained privately rather than committed.

## Checked behavior

- Operator identity/group lifecycle, direct membership, relationship reads, self-service tokens, explicit confirmations, and binding observation/recovery use the public API.
- Literal handle prefixes preserve punctuation and bind cursors to filters and parent resources. Disabled identities/groups and revoked/retired assignments remain distinct from effective authority.
- Current-token metadata reveals only an identifier. Token/session expiry or revocation, identity disablement, and role removal stop subsequent export pages.
- Exports retain bounded pages, preserve ordinary admission/authentication deadlines, and abort the transport after a streaming failure. Native browser downloads verify completion and interruption rather than treating initiation as success.
- Browser checks cover one-time secrets, current-token revocation, last-operator rejection, self-demotion, unknown writes without automatic replay, explicit read retries, partial authority pages, keyboard focus, narrow forms, and native 200 percent Chromium zoom.

## Reproducible checks

Toolchains: Go 1.26.5; Node 26.8.2/npm 11.19.1; Playwright 1.63.0 with Chromium 153.0.8010.12 and WebKit 26.6; PostgreSQL 16.14 and 18.4. Container frontend builds use the repository's Node 24 image.

| Check | Result |
| --- | --- |
| `go test -race ./...` and `go vet ./...` | Passed. |
| `make generate-check` | Passed; OpenAPI and SQL generation reproduce checked-in artifacts. |
| `go test -race -tags=integration -timeout=10m ./internal/database ./internal/httpserver` | Passed against both PostgreSQL versions. |
| `make benchmark-check` and `make benchmark-postgres-check` | Passed existing smoke gates; no performance claim inferred from a single iteration. |
| `make dev-contract integration-contract` | Passed. |
| `scripts/release_workflow_test.sh`, `scripts/footprint_test.sh`, `scripts/deploy-smoke.sh` | Passed. |
| Frontend type/unit/build and browser regressions | Passed: 11 unit tests; standard suite 22 passed / 2 opt-in skips; expanded management suite 33 passed / 1 WebKit skip for Chromium-native zoom. |
| `scripts/release_test.sh` | Passed: two identical builds per Linux architecture, checksum verification, and rejection of oversized or modified binaries. |
| OCI smoke, Linux amd64 and arm64 | Passed; root filesystems 23,535,616 and 22,007,808 bytes, below the 27,262,976-byte budget. |
| `scripts/integration.sh` and its runtime/resource gates | Passed unchanged for PostgreSQL 16.14 and 18.4, including DNS queries, authorization/lifecycle, dependency failure injection, recovery, and credential-free runtime logs. |

The live browser fixture is opt-in through `CONSOLE_LIVE_URL` and `CONSOLE_LIVE_TOKEN_FILE`; seed only an isolated database using `internal/benchtest/testdata/management.sql`. Management checks run with `npx playwright test -c playwright.management.config.ts`; `CONSOLE_WEBKIT=1` includes WebKit. The normal `npm run test:browser` command includes the management regressions and skips explicitly unconfigured live fixtures. `CONSOLE_TEST_PORT` selects an unused test port; existing servers are never reused.

The full integration harness ran in an isolated Linux/aarch64 virtual test environment and measured the actual application processes. These are virtual-machine measurements. Each database variant collected 15 restart samples and five ready-idle process samples. Readiness median/p95 was 670/1,480 ms on PostgreSQL 16 and 730/1,380 ms on PostgreSQL 18. Idle RSS maxima were 28,572 and 27,820 KiB, below the 65,536-KiB budget. Owned fixture processes, containers, images, networks, and volumes were cleaned; the development stack was preserved.

## Synthetic capacity

The seeded workload contains 1,000 identities, 100 groups, 1,000 members in one group, 100 tokens per identity (100,000 total), and 100,000 audit events. Browser-created fixtures add a few resources after seeding; these counts are validation targets, not enforced limits.

| Measurement | Observed result |
| --- | --- |
| Largest management collection response | 100 rows; 30,762 bytes across the two live browser runs. |
| Chromium completed audit download | 100,000 rows, 28,500,000 bytes; download plus validation 3.360 s. |
| WebKit completed audit download | 100,000 rows, 28,500,000 bytes; download plus validation 3.496 s. |
| Backend 200-page export with heap sampling | 13.510 s; sampled Go heap growth 2,775,208 bytes. |
| Independent streaming HTTP comparison | 100,000 rows with either header authentication or a browser-session cookie. |

These observations depend on the synthetic workload and local environment. The backend heap sample is not a hard process-memory ceiling or a browser-memory measurement. The browser uses a native download instead of a full application Blob. A stalled initial browser run was traced to synchronous process sampling and per-row assertion bookkeeping in the test harness; removing that overhead preserved complete line validation and allowed both browser runs to pass.

The original 400-zone/250,000-RRset design remains unchanged. Its previous capacity evidence is in the first console change's verification note; this slice reruns existing regressions rather than claiming a new large-zone capacity measurement.

## Review and corrections

Independent code review covered the exact implementation diff and relevant caller context. All 33 selected entries were accounted for: 31 source/configuration entries were manually reviewed; two minified generated bundles were verified through source review and deterministic rebuild instead (93.9 percent manual coverage). Documentation, tests, generated API code, fixture SQL, and obsolete asset deletions were inspected separately. Unrelated local tooling and raw operational evidence were excluded from the deliverable. Backend and frontend source hashes remained unchanged after review.

Confirmed issues were corrected: the browser output directory used a nonportable absolute path; native-zoom tests hardcoded a different server port from the normal suite; failed collection reads lacked an explicit retry; asynchronous detail headings missed navigation focus. Test configuration now uses relative output and the configured server, and shared UI controls provide retry and detail focus. Related regression checks passed in the final browser runs. A screenshot captured during a modal fade was corrected by waiting for animation completion; the focused zoom check and stable visual inspection passed. No confirmed review findings remain unresolved.

No native Safari application run, exhaustive HTTP/2 transport matrix, or real stalled-client deadline measurement is claimed. WebKit exercised HTTP development sign-in, one-time token interaction, complete downloads, and interrupted downloads; native HTTP tests cover transport abortion, cancellation, bounded page deadlines, and simulated write failures.
