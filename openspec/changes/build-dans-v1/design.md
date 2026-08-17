## Context

DANS is a greenfield control-plane service. See [proposal.md](proposal.md) for the product motivation. Its security boundary is one PowerDNS Authoritative 5.1 upstream whose native API has a single server-wide key and no RRset-level authorization. DANS must therefore be the only supported API/control-plane path to that upstream.

Authoritative DNS serving does not depend on DANS availability. DANS may stop management reads and mutations when its database or upstream is unavailable without interrupting existing DNS answers. The accepted domain language and architectural choices are recorded in `CONTEXT.md` and ADRs 0001–0010.

## Goals / Non-Goals

**Goals:**

- Make every exposed PowerDNS and DANS operation explicitly classifiable and fail closed.
- Authorize a complete RRset mutation batch from one current PostgreSQL snapshot before forwarding it once.
- Preserve PowerDNS response compatibility while giving DANS routes a strict generated contract.
- Keep identity, delegation, lifecycle, token, and audit state durable and consistent across multiple DANS instances.
- Make the public generated client and compiled CLI the same interfaces used by production code and integration QA.
- Keep deployment recoverable through explicit migrations, bootstrap, backup, restore finalization, and coordinated upgrades.
- Establish measured artifact, startup, idle-resource, and representative request-path baselines before accepting performance claims or footprint growth.

**Non-Goals:**

- Multiple PowerDNS upstreams, upstream write failover, or a queued/offline mutation path.
- Regex policies, explicit denies, nested groups, per-record-value ownership, or field-level RRset ownership.
- Read filtering among authenticated identities; ordinary DNS data is shared-read.
- Automatically forwarding unknown future PowerDNS routes or fields.
- Supporting direct PowerDNS API writers, automatic zone-generation discovery after out-of-band lifecycle changes, or mutation replay after ambiguous outcomes.
- A web UI, application metrics/tracing, a Helm chart/operator, PostgreSQL bundling, or mixed-version rolling upgrades in v1.

## Decisions

### 1. One pinned and classified API contract

Vendor the unmodified PowerDNS 5.1.3 OpenAPI 3.1 source with its checksum, then apply a small checked-in DANS overlay. The overlay adds `/api/v1/dans` resources, generator naming fixes, evidence-backed request-schema corrections, and an access-class extension on every operation. A build-time completeness check compares operation IDs from the bundled contract with the access classification and rejects missing or extra entries.

Pin `oapi-codegen` v2.8.0 as a Go tool and commit deterministic generated models, the standard-library server surface, strict DANS handler interfaces, and the client. The upstream adapter uses the generated PowerDNS client directly. Compatibility handlers retain access to the raw upstream `http.Response` so response bodies are not converted through generated models.

Alternatives rejected:

- Runtime discovery from PowerDNS `/api/docs` would make authorization change when the upstream changes and cannot be reviewed before exposure.
- A generic reverse proxy would authorize a different representation from the request forwarded upstream.
- Ogen generated a substantially larger runtime surface in the contract smoke test without improving response fidelity for this gateway.

### 2. Explicit HTTP request pipeline

Use one `net/http` server with this trust-boundary order:

1. Assign a DANS request ID, recover panics, bound headers/body/time, and match a declared route.
2. Validate method, path, query, media type, and body against the bundled contract; reject duplicate JSON keys and unknown DANS fields.
3. Extract exactly one syntactically valid `X-API-Key` without logging it.
4. Decode the request once and execute the route-class authorization statement against the PostgreSQL primary.
5. For compatibility routes, construct one validated upstream request, replace the client key with the configured PowerDNS key, and forward it once.
6. Record the observed result and return either the unwrapped upstream response or a DANS error.

All DANS-originated failures use the PowerDNS-compatible `{error, errors?}` body and the documented status mapping. Authenticated responses set `Cache-Control: no-store`; every response includes `X-Request-ID`. Request forwarding uses an explicit end-to-end header allowlist and strips client authentication, `Host`, forwarding headers, and RFC-defined hop-by-hop headers. Mutation timeouts are never retried because a timeout can conceal a committed change.

The three PowerDNS access classes are fixed by operation ID:

- authenticated DNS read: zone list/get/export and search;
- delegated write: zone `PATCH` only;
- operator-only: every other pinned operation.

DANS management operations have their own operator or self-service checks. `/api/docs` serves the bundled contract to authenticated callers. `/livez` and `/readyz` are the only anonymous routes.

### 3. Canonical DNS names and deterministic selectors

At the management boundary, convert valid Unicode domain input with IDNA2008, lowercase the resulting ASCII A-label form, and store absolute names with a trailing dot. Decode supported PowerDNS DNS presentation escapes before comparison, preserve legal non-hostname labels, and perform zone containment by DNS labels rather than textual suffix.

A selector is tagged `exact` or `glob`. Glob syntax has only `*` (zero or more canonical-name characters) and `?` (one character); matching can cross dots, as in Route 53 IAM patterns. Store the canonical selector and a creation-time escaped SQL `LIKE` form. Exact selectors alone may authorize the zone apex or a literal DNS wildcard RRset. PTR is treated like every other RR type: authorization matches its reverse owner name, not its RDATA. CIDR-to-PTR sugar is not part of v1.

The zone `PATCH` request model corrects the upstream schema where official PowerDNS 5.1.3 examples permit fields to be absent for `DELETE`, `EXTEND`, and `PRUNE`. DANS authorizes TTL, records, disabled state, comments, and supported write flags as one RRset; PowerDNS remains responsible for record-content validation.

### 4. Relational authorization state and one decision statement

PostgreSQL is the only state engine. The initial schema contains:

- installation/schema metadata and one row used to serialize the last-enabled-operator invariant;
- identities, API tokens, groups, and direct group memberships;
- immutable-generation zone bindings;
- delegations with selector, record-type, and change-kind child rows;
- append-only audit events.

Generate UUIDv4 resource IDs and token secrets in Go with `crypto/rand`, avoiding a database extension. Use `timestamptz`, text plus check constraints, foreign keys with restrictive lifecycle behavior, and partial unique indexes for active uniqueness. Identities, groups, tokens, bindings, and delegations are disabled, revoked, or retired rather than hard-deleted.

Use native `pgx/v5` pooling and checked-in `sqlc` queries without an ORM, query builder, repository interface, PostgreSQL enum, `citext`, RLS, or stored procedure. Short `READ COMMITTED` transactions and row locks protect management invariants. A mutation that can change operator availability locks the singleton guard first and rejects a result with zero enabled operators.

Each request class makes one final primary-database authorization statement. Delegated writes pass a bounded JSONB array of canonical `(owner,type,change-kind)` tuples; the query authenticates the token and identity, recognizes operators, unions current direct and enabled-group grants, excludes inactive bindings or revoked delegations, and requires every tuple to match. SQL `LIKE` matching uses an explicit escape character and C-compatible canonical ASCII semantics. Authorization results are not cached or read from replicas.

### 5. Opaque credentials and management resources

Tokens have the form `dans_v1_` plus 32 random bytes encoded as unpadded base64url. Store an indexed SHA-256 digest of the complete token plus its immutable metadata; return the plaintext only from creation. Do not persist `last_used_at`, encrypt token values, add a password KDF, or place authority claims in the token. Disabled identities, expiry, revocation, role changes, membership changes, and delegation changes therefore affect the next authorization decision point.

The management API uses lowercase UUIDv4 IDs in paths and immutable lowercase ASCII handles. It exposes identities, nested identity tokens, groups, idempotent direct memberships, immutable delegations, zone bindings, audit pages, and `/me` self-service pages. An enabled identity can inspect its own profile, groups, effective delegations, and token metadata and can create/revoke its own tokens. Operators manage all resources. Collection responses use bounded keyset cursors and do not promise total counts or cross-page snapshots.

The same binary provides exclusive PostgreSQL maintenance commands:

- bootstrap an uninitialized installation with one human operator and one token;
- recover a token only for an existing enabled operator;
- finalize a supported restore by revoking every restored token and issuing one replacement;
- migrate or report schema status using a DDL-capable credential.

These operations take database-wide locks where required, emit audit events, print a new secret once, and have no HTTP backdoor.

### 6. Append-only audit and ambiguous outcomes

Use one append-only audit ledger. A management mutation and its completed audit event commit in the same transaction. A PowerDNS mutation first commits an immutable intent with an operation ID and deadline, then performs exactly one upstream call and appends exactly one terminal `succeeded`, `failed`, or `unknown` outcome. Unique constraints prevent duplicate intent or outcome events.

Store actor/token IDs, request ID, action, typed target, normalized RRset summaries, matched delegation IDs, response class/code, and request/response digests. Never store credentials, secret-bearing bodies, or complete DNS request/response bodies. Authenticated authorization denials are durable audit events; ordinary successful reads use structured access logs.

Any instance may close an overdue unmatched intent as `unknown` with an idempotent insert. It never polls PowerDNS or replays the operation. Failure to persist an intent prevents forwarding. If PowerDNS already returned but outcome persistence fails, return the observed upstream response, make readiness fail, and preserve the unmatched intent for reconciliation rather than invite a client retry.

Retain audit events indefinitely in v1. Operators read keyset-paginated events through the API, and the CLI streams NDJSON for external archiving. Automated pruning is deferred until a concrete retention requirement exists.

### 7. Zone generation lifecycle is fail-safe

A zone binding records the configured upstream, opaque PowerDNS zone ID, canonical-name snapshot, and an immutable generation. Create it after DANS creates a zone or lazily when an operator first delegates an existing zone. Concurrent binding attempts converge through database uniqueness and conditional insertion.

Before forwarding a zone deletion, retire the binding, revoke all attached delegations, and commit the audit intent. An upstream success or not-found completes deletion. Any failure or timeout leaves the binding retired and authority revoked. Reconciliation performs observation only; restoring authority or addressing a recreated zone requires an explicit new binding and new grants.

DANS is the sole supported PowerDNS API/control-plane writer. A request whose authorization decision committed before a concurrent revocation may finish, but later decisions observe the revocation. No database transaction or lock is held across a PowerDNS network call. Concurrent RRset mutations therefore retain PowerDNS's last-successful-write behavior.

### 8. One CLI and immutable startup configuration

Build a fresh Cobra command tree for each invocation and use `ExecuteContext` with injectable standard streams. The command surface covers `serve`, database migration/status, bootstrap/recovery/restore, every DANS resource, audit export, health/version/completion, and common zone/RRset workflows. It does not clone every rare PowerDNS operation and has no generic raw-request command. Online commands import only the generated public client, never server handlers or persistence packages.

Create a fresh Viper instance per command. Configuration precedence is built-in defaults, one optional explicitly named JSON file, explicitly bound `DANS_*` environment variables, then flags. Strictly decode one typed schema, validate only the selected command's needs, and discard Viper before starting goroutines. Do not use global Viper state, `AutomaticEnv`, implicit file search, alternate formats, remote providers, or live reload.

Secrets are supplied through a dedicated environment value or a mutually exclusive `*_file` path and are never legal literal flags or JSON values. The upstream key also supports one explicitly named rendered PowerDNS fragment containing exactly one plaintext `api-key` assignment. Read secrets once, trim one terminal newline, reject empty or malformed values, and redact them everywhere.

CLI output is deterministic `text` or `json`, with JSON as the stable automation contract. Success data goes only to stdout and errors only to stderr. Exit codes are 0 success, 1 runtime/API failure, 2 invocation/configuration failure, and 130 interruption. Commands never prompt or automatically retry a mutation.

### 9. Multi-instance runtime and operational boundary

Support PostgreSQL majors 16–18 on current minor releases and one PowerDNS `>=5.1.3,<5.2` upstream. Every DANS instance talks only to the PostgreSQL primary and the same upstream; pools, request concurrency, statement deadlines, and upstream deadlines are bounded by configuration. There is no policy cache, read replica, mutation queue, upstream failover, or automatic mutation retry.

`/livez` reports process health only. `/readyz` requires primary-database connectivity, a compatible schema, an auditable storage path, and a successful authenticated PowerDNS version probe; it returns only 200 or 503 without dependency detail. SIGTERM marks the instance unready, stops accepting new work, and drains bounded in-flight requests. JSON `slog` access/error logs carry request and opaque resource IDs but no bodies or credentials. Metrics and tracing are deferred.

Client TLS terminates at a trusted ingress. DANS remains private, and its PowerDNS connection uses a Unix socket when colocated or restricted loopback/private networking otherwise. The upstream PowerDNS listener and key are inaccessible to DANS clients.

### 10. Generated and real-system verification

CI validates and bundles the OpenAPI source plus overlay, regenerates code, runs `sqlc`, and fails on checked-in generation drift before compiling and testing. Unit/contract tests cover canonicalization, glob compilation and matching, token parsing, cursor validation, route-class completeness, header filtering, error mapping, and SQL authorization edge cases.

A required Linux integration job builds the real binary and uses a fresh environment with PostgreSQL, PowerDNS 5.1.3, and two same-version DANS instances. It runs migrations and bootstrap through the compiled CLI, drives management and DNS workflows through the generated client/CLI, and verifies resulting records with actual DNS queries and upstream state. It covers direct and group grants, forward and PTR RRsets, apex/literal-wildcard protection, mixed-batch denial, revocation across instances, sensitive-route denial, audit outcomes, dependency outages, schema mismatch, and ambiguous upstream results. Fault controls live only in the test environment; production exposes no seed, reset, introspection, or fault endpoint.

Publish a non-root OCI image and checksummed Linux amd64/arm64 binaries. The same image runs online and maintenance commands. Provide minimal Docker/Podman and Kubernetes examples without bundling PostgreSQL or PowerDNS.

### 11. Measure footprint before optimizing

Keep a small benchmark suite at actual supported boundaries: opaque-token validation/digesting, owner-name canonicalization, strict zone-patch decoding, authentication middleware, and the real PostgreSQL RRset authorization statement. Batch-sensitive cases cover one and 100 entries. Benchmarks use `testing.B.Loop`, fixed inputs outside the measured loop, allocation reporting, and post-loop correctness checks. The SQL case uses a disposable supported PostgreSQL instance with representative relational state rather than a mock.

The initial measured production baseline is:

| Metric | Measured baseline | Budget |
| --- | --- | --- |
| Linux amd64 executable | 20,459,646 bytes | 24 MiB |
| Linux arm64 executable | 19,005,566 bytes | 24 MiB |
| OCI amd64 unpacked rootfs | 20,709,376 bytes | 26 MiB |
| OCI arm64 unpacked rootfs | 19,255,296 bytes | 26 MiB |
| Offline `version` wall time, 50 runs | 6.399 ms median / 7.182 ms p95 | 10 ms median / 15 ms p95 |
| Offline maximum RSS, 50 runs | 16.004 MiB median / 18.043 MiB p95 | 24 MiB p95 |
| Warm start to readiness, 15 runs | 122.113 ms median / 137.960 ms p95 | 500 ms p95 |
| Ready-idle DANS process, five samples | 27.375 MiB VmRSS / 26 goroutines / 9 FDs | 64 MiB VmRSS; counts informational |

CI hard-gates deterministic executable size and the unpacked container root filesystem. Native-Linux integration verifies the generous ready-idle VmRSS ceiling, while goroutine and file-descriptor counts remain diagnostic evidence. Timing and allocation evidence uses at least five three-second benchmark runs and same-host statistical comparison; absolute shared-runner `ns/op` values are not hard gates. Store compact raw samples, exact commands, hashes, dependency digests, and summaries with the change. Do not add a production metrics or profiling endpoint for this baseline, and do not optimize paths that the evidence has not identified as a problem.

## Risks / Trade-offs

- **PowerDNS's published schema differs from accepted server requests** → Keep upstream source immutable, document every overlay patch, and verify patches against real 5.1.3 in CI.
- **An upstream timeout can hide a committed mutation** → Persist intent before forwarding, never retry automatically, record `unknown`, and require operator reconciliation.
- **Authorization depends on primary-database latency** → Keep one bounded indexed decision statement, avoid per-request writes and caches, and expose database failure through readiness.
- **SQL `LIKE` is a security boundary** → Canonicalize to ASCII, compile and escape patterns once, use explicit collation/escape semantics, and test adversarial wildcard and zone-containment cases.
- **Indefinite audit retention grows PostgreSQL storage** → Provide paginated export and operational size monitoring; add pruning only with an explicit retention policy.
- **A direct or recreated upstream zone can invalidate generation assumptions** → Restrict upstream writers, retire before delete, and require explicit rebind without grant inheritance.
- **Coordinated upgrades briefly stop management traffic** → Authoritative DNS remains available; add expand/contract mixed-version migrations only after a real rolling-upgrade requirement.
- **Supporting three PostgreSQL majors expands verification cost** → Keep SQL within the oldest supported feature set and test both oldest and newest majors on every change.
- **Timing and RSS vary across hosts and container runtimes** → Repeat measurements on a pinned runner, compare revisions on the same host, hard-gate only deterministic footprint and deliberately broad resource ceilings, and retain raw samples with the summary.

## Migration Plan

Initial deployment:

1. Restrict the PowerDNS API listener so only DANS can reach it, and supply the same plaintext key through an approved DANS secret source.
2. Create separate PostgreSQL DDL and runtime roles, then run the target DANS binary's locked migration command.
3. Run bootstrap once and capture the one-time operator token.
4. Start all same-version DANS API instances, verify readiness, and route client traffic through the trusted TLS ingress.
5. Create bindings and delegations for existing zones through the public management API.

Upgrade deployment:

1. Back up PostgreSQL and verify the backup.
2. Drain and stop every DANS instance.
3. Run the target binary's forward-only migrations with the DDL role.
4. Start only that target version and verify readiness before reopening traffic.

Before migration, rollback means restarting the prior binary. After a migration, binary downgrade is unsupported; restore the verified database backup, run restore finalization to revoke restored tokens and issue one replacement operator token, then start the matching prior binary.
