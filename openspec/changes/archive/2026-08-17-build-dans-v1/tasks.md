## 1. Go and contract foundation

- [x] 1.1 Initialize the Go module, pin the supported Go toolchain, and add the minimal command/internal package layout for one `dans` executable.
- [x] 1.2 Pin Cobra, Viper, `pgx/v5`, `sqlc`, `oapi-codegen` v2.8.0, OpenAPI request validation, and IDNA support using Go tool directives where applicable.
- [x] 1.3 Vendor the exact PowerDNS 5.1.3 OpenAPI 3.1 source and record a checksum plus an update/verification command.
- [x] 1.4 Create the checked-in DANS OpenAPI overlay with `/api/v1/dans` resources, health/docs routes, security schemes, error responses, and evidence-backed PowerDNS request-schema corrections.
- [x] 1.5 Attach an explicit access class to every combined-contract operation and add a completeness test that rejects missing, extra, or unknown operation IDs.
- [x] 1.6 Add deterministic bundle and `oapi-codegen` configuration, generate the combined models/client/server surfaces, and make generation drift fail locally and in CI.
- [x] 1.7 Add contract tests that prove legal PowerDNS `DELETE`, `EXTEND`, and `PRUNE` RRset bodies validate while undeclared request extensions remain rejected.

## 2. PostgreSQL schema and migration lifecycle

- [x] 2.1 Implement the locked, forward-only embedded migration runner and checksummed `schema_migrations` ledger used only by CLI maintenance commands.
- [x] 2.2 Add the installation metadata and last-enabled-operator guard schema with database constraints for one initialized installation.
- [x] 2.3 Add identity, group, direct-membership, and API-token tables with UUID IDs, lifecycle fields, active uniqueness, foreign keys, and no hard-delete path.
- [x] 2.4 Add immutable-generation zone binding and delegation tables, including selector, record-type, and change-kind child rows and active/revoked/retired constraints.
- [x] 2.5 Add the append-only audit event schema with operation intent/outcome uniqueness and privileges that prevent runtime update or deletion.
- [x] 2.6 Define separate DDL and runtime database privileges and document the grants needed by migration and API instances.
- [x] 2.7 Configure `sqlc` for native `pgx/v5`, commit generated queries, and add generation-drift enforcement without an ORM or repository interface.
- [x] 2.8 Add PostgreSQL integration tests for uniqueness, foreign keys, lifecycle constraints, append-only audit permissions, and concurrent migration locking.

## 3. Security and DNS domain primitives

- [x] 3.1 Implement UUIDv4 and `dans_v1_` token generation with `crypto/rand`, exact syntax validation, SHA-256 lookup digests, and deterministic tests that never expose stored plaintext.
- [x] 3.2 Implement canonical absolute DNS-name parsing, IDNA2008 A-label conversion, ASCII case folding, presentation-escape handling, and label-aware zone containment with table-driven tests.
- [x] 3.3 Implement tagged exact/glob selector validation and escaped SQL `LIKE` compilation for `*` and `?`, with adversarial tests for `%`, `_`, dots, escapes, apexes, and literal wildcard owners.
- [x] 3.4 Implement bounded versioned keyset cursor encoding/decoding and tests for malformed, wrong-resource, overlong, and state-changing-page cases.
- [x] 3.5 Implement stable DANS error/status mapping, request-ID generation, cache-control behavior, and secret-safe diagnostic helpers with redaction tests.
- [x] 3.6 Implement strict JSON duplicate-key and unknown-field rejection for DANS request bodies and verify malformed requests cannot reach authorization or upstream forwarding.

## 4. Identity, token, and group persistence

- [x] 4.1 Implement the single-statement token authentication query for active tokens and enabled identities with uniform unusable-credential behavior.
- [x] 4.2 Implement identity create/list/get/patch transactions, including immutable kind/handle fields and the locked last-enabled-operator invariant.
- [x] 4.3 Implement group create/list/get/patch and idempotent direct-membership add/remove queries with disabled-group authority semantics.
- [x] 4.4 Implement operator-managed and self-service token list/create/revoke flows with one-time secret responses, optional future expiry, and no last-use writes.
- [x] 4.5 Implement paginated `/me`, identity, group, membership, and token reads with the accepted filters, ordering, and caller-visibility rules.
- [x] 4.6 Add transaction/concurrency tests for simultaneous operator demotions, identity disable/re-enable, duplicate handles, duplicate active token labels, and membership races.

## 5. Delegation authorization engine

- [x] 5.1 Implement operator-only delegation creation validation for one active binding, one identity/group grantee, non-empty selectors, and optional non-empty type/action restrictions.
- [x] 5.2 Implement immutable delegation reads and idempotent revocation while retaining historical state and overlapping grants.
- [x] 5.3 Implement the bounded one-statement delegated-write decision that revalidates token/identity/binding state, unions direct and group grants, and requires every canonical batch tuple to match one complete grant.
- [x] 5.4 Return all denied batch indexes with submitted owner/type/change kind while keeping delegation IDs, selectors, and other grantees private.
- [x] 5.5 Add authorization tests for exact/glob semantics, cross-label matching, zone containment, apex/literal-wildcard protection, every change kind, unrestricted/all RR types, PTR owners, and restrictions that cannot be composed across grants.
- [x] 5.6 Add cross-connection tests proving current authority, operator bypass, overlapping grants, revoke-before-decision denial, and allowed completion after an earlier decision point.

## 6. Audit and zone-generation lifecycle

- [x] 6.1 Implement atomic completed audit events for management changes and authenticated authorization denials with secret-safe metadata and digests.
- [x] 6.2 Implement DNS mutation intent creation before forwarding and exactly-one terminal `succeeded`/`failed`/`unknown` outcome insertion.
- [x] 6.3 Implement the bounded overdue-intent closer using idempotent insertion, without PowerDNS polling or mutation replay.
- [x] 6.4 Implement operator-only keyset-paginated audit reads and streaming NDJSON export with no delete/prune operation.
- [x] 6.5 Implement active zone-binding creation after DANS zone creation and safe lazy binding for an existing PowerDNS zone, including concurrent convergence.
- [x] 6.6 Implement revoke-and-retire-before-delete, then one upstream delete whose success/not-found completes and whose failure/timeout preserves retired authority.
- [x] 6.7 Implement explicit observe/reconcile/retry/rebind operations that never reactivate an old binding or copy old delegations.
- [x] 6.8 Add crash/failure/concurrency tests for missing audit storage, duplicate outcome closers, outcome persistence failure after an observed response, lazy bind versus retirement, delegation creation versus retirement, and recreated-zone authority.

## 7. PowerDNS upstream gateway

- [x] 7.1 Implement upstream configuration for Unix-socket or restricted HTTP transport and mutually exclusive PowerDNS key file/rendered-fragment sources with strict parsing and redaction.
- [x] 7.2 Implement authenticated startup/readiness probes that accept PowerDNS `>=5.1.3,<5.2` and fail closed for missing, rejected, or incompatible upstreams.
- [x] 7.3 Implement typed upstream request construction with validated path/query/body values, a request-header allowlist, client-key replacement, and forwarding/hop-by-hop header removal.
- [x] 7.4 Implement raw upstream status/body/end-to-end response relay while stripping response hop-by-hop headers and adding only DANS-owned security/tracing headers.
- [x] 7.5 Implement connection-failure, upstream-auth-failure, timeout, and authorization-dependency error mapping without wrapping genuine upstream HTTP errors.
- [x] 7.6 Add gateway tests proving one forwarded call per mutation, no automatic retries, exact authorized/forwarded RRset values, undeclared response-field preservation, and credential/header isolation.

## 8. HTTP server and public management API

- [x] 8.1 Assemble the bounded `net/http` middleware pipeline for request IDs, recovery, body/header/time limits, contract validation, credential extraction, authorization, access logging, and response security.
- [x] 8.2 Implement anonymous detail-free `/livez` and `/readyz`, authenticated combined `/api/docs`, and graceful readiness-first shutdown with a bounded drain.
- [x] 8.3 Implement the PowerDNS operation handlers with their exhaustive authenticated-read, delegated-patch, and operator-only classifications.
- [x] 8.4 Implement strict DANS identity, token, group, membership, and `/me` handlers using generated request/response types only.
- [x] 8.5 Implement strict DANS zone-binding, delegation, audit, reconciliation, and zone-lifecycle handlers using generated request/response types only.
- [x] 8.6 Add HTTP contract tests for statuses, locations, pagination, filtering, explicit-null PATCH behavior, uniform authentication errors, resource invisibility, conflicts, validation failures, and request IDs.
- [x] 8.7 Add HTTP security tests for sensitive PowerDNS reads, non-operator administration, duplicate API-key headers, malformed tokens, disabled identities/groups, expired/revoked tokens, and no-store responses.

## 9. Cobra CLI and immutable configuration

- [x] 9.1 Build a fresh Cobra root per invocation with `ExecuteContext`, injectable streams, offline help/version/completion, and single-point error/usage rendering.
- [x] 9.2 Implement fresh-Viper configuration loading with optional `--config`/`DANS_CONFIG`, strict JSON, explicit known environment/flag bindings, accepted precedence, and command-scoped validation.
- [x] 9.3 Implement mutually exclusive environment or `*_file` secret loading, terminal-newline handling, size/NUL/empty checks, and tests that reject literal JSON/flag secrets.
- [x] 9.4 Implement deterministic text/JSON output, NDJSON audit streaming, stdout/stderr separation, and stable exit codes 0/1/2/130 without prompts or mutation retries.
- [x] 9.5 Implement database migration/status, bootstrap, operator-token recovery, and restore-finalization commands with explicit destructive confirmation and one-time secret output.
- [x] 9.6 Implement generated-client-backed online commands for identities, groups, memberships, tokens, delegations, zone bindings, reconciliation, audit, and self-service.
- [x] 9.7 Implement generated-client-backed common zone and RRset workflows, including individual-value operations, without a raw-request escape hatch or a clone of every rare PowerDNS operation.
- [x] 9.8 Add CLI tests with fresh command trees for config precedence, secret conflicts, output/exit contracts, offline side-effect freedom, cancellation, and one-time token safety.

## 10. Multi-instance runtime and observability

- [x] 10.1 Implement bounded primary-only PostgreSQL pooling, statement/lock/transaction deadlines, and reserved capacity for authentication and readiness.
- [x] 10.2 Implement JSON `slog` request/runtime logging with route template, operation ID, status, duration, opaque actor/resource/operation IDs, and upstream outcome while excluding bodies and credentials.
- [x] 10.3 Implement readiness degradation for database, schema, audit-persistence, or PowerDNS probe failure without failing process liveness or restarting solely for dependency outages.
- [x] 10.4 Add two-instance tests for immediate cross-instance role, group, delegation, token, and identity-state changes without authorization caches or replicas.
- [x] 10.5 Document and test coordinated upgrade behavior, including refusal of incompatible schemas and unsupported mixed-version/downgrade operation.

## 11. Real-system integration QA

- [x] 11.1 Create a pinned Linux integration environment with current PostgreSQL 16 and 18 minors, PowerDNS 5.1.3, two same-version DANS instances, isolated networks, and a fresh project/database per run.
- [x] 11.2 Build the real executable, run migrations/bootstrap through its CLI, wait on readiness without sleeps, and drive online setup only through the compiled CLI or generated client.
- [x] 11.3 Add end-to-end direct/group delegation flows for forward and PTR RRsets, `REPLACE`/`DELETE`/`EXTEND`/`PRUNE`, overlapping grants, comments/TTL, and actual DNS-query verification.
- [x] 11.4 Add end-to-end denials for apex/literal wildcard access, mixed batches, revoked/current authority across instances, secret-bearing routes, server operations, and management visibility.
- [x] 11.5 Add end-to-end audit and zone-lifecycle flows for successful/failed/unknown mutations, delete retirement, same-name recreation, reconciliation, rebind, and no grant inheritance.
- [x] 11.6 Add test-environment-only fault controls for PostgreSQL loss, PowerDNS loss, rejected upstream key, response timeout after forwarding, audit-outcome failure, and schema mismatch; assert no bypass, queue, or replay.
- [x] 11.7 Collect service/container logs on failure and assert production artifacts expose no test seed/reset/introspection/fault routes.
- [x] 11.8 Make the integration matrix and generation/unit/race/static-analysis checks required for every accepted change.

## 12. Packaging, operations, and release documentation

- [x] 12.1 Build a non-root OCI image whose one executable supports serve and maintenance commands, and verify it on Linux amd64 and arm64.
- [x] 12.2 Publish checksummed Linux amd64/arm64 executables and add an automated integrity check for release artifacts.
- [x] 12.3 Add minimal Docker/Podman and Kubernetes examples with trusted TLS ingress, private DANS networking, restricted PowerDNS reachability, external PostgreSQL, and mounted secret files.
- [x] 12.4 Document initial migration/bootstrap, backup, restore finalization, token rotation/recovery, coordinated upgrade/rollback, and failed/unknown mutation reconciliation runbooks.
- [x] 12.5 Document the public API/client and CLI workflows, policy examples for forward/PTR/apex/wildcard names, route classes, and explicit v1 limitations.
- [x] 12.6 Run strict OpenSpec validation, all generation-drift checks, unit/contract/integration tests, race/static analysis, and artifact smoke tests; record the verified release baseline.

## 13. Generated-code review remediation

- [x] 13.1 Add bounded defaults for supported generated-client entry points and enforce a finite response-body limit before generated response parsing, with timeout, oversize, custom-transport, and body-close tests.
- [x] 13.2 Configure `sqlc` to generate nullable PostgreSQL UUIDs consistently as `*string`, regenerate database artifacts, and verify generation drift plus nullable scan behavior.
- [x] 13.3 Reconcile the audit terminal-outcome vocabulary with the accepted `succeeded`/`failed`/`unknown` contract and validate the strict OpenSpec change.
- [x] 13.4 Re-run focused OCR review, generation checks, unit and race tests, vet/static checks, and strict OpenSpec validation; update the verified baseline.

## 14. Measured performance and resource baseline

- [x] 14.1 Add normative performance/resource requirements and evidence-backed budgets for release binary size, OCI footprint, startup/readiness, idle memory, and representative request-path latency/allocations.
- [x] 14.2 Add minimal representative Go benchmarks using `b.Loop`, fixed inputs, and `ReportAllocs`; capture repeated raw runs and statistical summaries without optimizing unproven paths.
- [x] 14.3 Add a reproducible footprint measurement command for production artifacts and a pinned real DANS/PostgreSQL/PowerDNS runtime, recording toolchain, platform, repetitions, medians, and ranges.
- [x] 14.4 Enforce deterministic size/resource ceilings in CI while keeping noisy timing comparisons evidence-based and same-host rather than absolute shared-runner gates.
- [x] 14.5 Run the performance baseline, correctness/race checks, real-system smoke, and strict OpenSpec validation; record the measured release baseline and any intentionally deferred optimization.
