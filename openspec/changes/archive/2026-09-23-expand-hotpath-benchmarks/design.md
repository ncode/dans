## Context

See [proposal.md](proposal.md) for motivation. The existing suite already uses allocation reporting and post-loop assertions. Its database fixture supplies one connection, and the authorization benchmark measures an allowed batch against one direct grant. CI runs one iteration per case.

Production requests include work outside those measurements: contract validation, database authentication, a database compatibility check, access logging, and request limits. RRset writes additionally decode the body, authorize the batch, persist an intent, forward upstream, and persist an outcome. Selector matching occurs in SQL; the Go selector matcher has no production callers.

## Goals / Non-Goals

**Goals:**

- Give each result an explicit boundary: isolated Go work, real database work, or a complete request with a controlled upstream.
- Compare representative fixed workloads without timing fixture construction or accidentally measuring an error shortcut.
- Keep the suite maintainable by extending existing tests and using standard Go benchmark facilities.

**Non-Goals:**

- Optimizing production code, declaring capacity or latency targets, or interpreting candidates as confirmed bottlenecks.
- Adding a benchmark framework, production profiling routes, or a new runtime dependency.
- Benchmarking every generated endpoint, management operation, or uncalled helper.

## Decisions

### Measure shared paths with explicit boundaries

Use package-local benchmark files and extend existing helpers to accept `testing.TB` only where they are actually shared. Keep any integration fixture adaptation in test code; do not export production helpers solely for benchmarks.

| Boundary | Measurement | Initial cases |
| --- | --- | --- |
| Go primitives | Existing token, DNS, and strict JSON benchmarks | Preserve current cases; add Unicode and escaped DNS owners, legal long names, larger record arrays, and late JSON rejection |
| Contract middleware | `NewContractValidation` with a terminal handler | Authenticated read shape; valid PATCH at 1/10/100 RRsets; duplicate keys, unknown fields, and a near-limit valid body |
| Portable complete request | `NewApplicationHandler`, labeled stub database dependencies, loopback upstream | Read; PATCH at 1/10/100 RRsets; partial denial |
| Database checks | `Store.Authenticate` and `CheckRuntimeCompatibility` | Valid, unknown, revoked, and expired credentials; healthy compatibility check |
| Database authorization | Existing `AuthorizeRRsetBatch` benchmark | Direct/group grants, exact/glob selectors, overlapping matches, partial/full denial, and grant populations of 1/100/1000 |
| Database audit | `CreateDNSIntent` followed by `RecordDNSOutcome` | Committed pairs for 1/100 RRsets; success and failure outcomes; authorization denial measured separately |
| Response relay | `Relay` plus generated-client forwarding over loopback HTTP | 1 KiB/64 KiB/1 MiB bodies, success/error responses, ordinary/filtered headers, warm connection reuse |
| PostgreSQL complete request | Production handler with real stores and runtime pools, loopback upstream | Read; allowed PATCH at 1/100 RRsets; denied PATCH |
| Concurrent complete request | Same PostgreSQL handler and pools with worker-local requests and writers | Reads, writes, and a fixed 9-read/1-write mix at bounded concurrency |

These are selected scenarios, not a Cartesian product. For authorization, vary grant population while holding batch size and grant shape fixed, then vary batch size with a fixed population. Include nonmatching candidates and overlapping matches so the population changes actual query work. Reuse semantic examples from existing correctness tests.

The default workload assumption is mixed API traffic; the 9:1 case is a reproducible synthetic workload, not a claim about observed traffic. Keep separate read and write results so a different production mix can be assessed later.

Complete-request cases execute contract checks, default request-ID generation, authentication, compatibility, authorization, and logging. Send encoded logs to `io.Discard` to bound memory; this includes log formatting but excludes the cost of a deployment's log destination. Portable cases label database dependencies as stubs. PostgreSQL cases wire real stores and compatibility queries using the runtime pool configuration. Report loopback server/client allocations as part of those process-wide Go measurements, never as database-server memory.

Alternatives considered: measuring every route would duplicate shared machinery; measuring only helpers would omit repeated validation, database round trips, and commits. A live external upstream would add uncontrolled latency and require access to mutable external data.

### Keep fixtures fixed and assert the real result

Prepare payloads, relational seed state, routers, handlers, and clients before timing. Use serial `b.Loop` with `ReportAllocs`, and `SetBytes` where a fixed payload size makes throughput meaningful. Reset consumed request bodies and response headers/status on every operation; include necessary per-request work and document harness allocations.

Validate actual results after the loop: decoded/canonical values, allowed and denied tuple details, matched grants, complete response bytes and status, blocked headers, and expected upstream/audit counts. Preserve the first unexpected error so a later success cannot hide it. Keep recording stubs bounded rather than appending every request to a slice.

Do not let `bytes.Reader.WriteTo` or a discard writer's fast path stand in for network streaming. Include a loopback transport case with fully drained and closed responses, a warmed connection, and connection counts, extending the existing deletion benchmark pattern. Avoid per-operation `httptest.NewRecorder` body growth when measuring relay allocations; use a reusable bounded counting/checking writer.

### Use fixed work for benchmarks that append durable state

Keep read-only benchmarks duration-based. Give audit-writing, denied-authorization, PostgreSQL PATCH, and mixed-traffic benchmarks distinct names so commands can select them separately.

Run state-mutating evidence with an explicit fixed iteration count and a fresh disposable schema per sample. Use a pilot to choose enough work for a stable measurement, then use that identical count for both revisions. Record initial/final row counts and validate the expected delta. Seeded input sizes remain independent of the benchmark iteration count.

Timed mutation operations retain real commits, constraints, and indexes. Cleanup runs after timing with a bounded independent context. Intent deadlines must remain valid throughout a sample. Do not replace commits with an outer rolled-back transaction, perform per-operation truncation, or allow inherited state from an earlier sample to change the workload. A plain duration-based run remains a correctness smoke for these cases, not accepted comparison evidence.

### Exercise concurrency through actual shared pools

Use `b.RunParallel` and `pb.Next` for concurrent benchmarks, with explicit timer reset after setup; serial cases continue to use `b.Loop`. Update the existing specification's blanket loop rule to permit this standard parallel runner.

Use worker-local request bodies, writers, and counters. Share bounded production-style pools rather than the single `pgx.Conn` used by the current serial fixture. Fix and record pool sizes, request limits, worker count, and `GOMAXPROCS` independently. For the mixed case, use a fixed per-worker operation sequence; aggregate the observed read/write counts so the effective mix can be checked across revisions.

Workers perform synchronous operations with request deadlines and finish before the benchmark returns. Aggregate failures without calling `Fatal` from a worker. Register cleanup for pools, transports, and servers, and exercise the benchmark bodies under the race detector using a short separate run. Parallel `ns/op` describes aggregate throughput, not a request-latency percentile.

### Preserve fast CI checks and capture reproducible evidence separately

Extend the existing portable smoke target to include `internal/upstream`. Extend the PostgreSQL smoke target to include new database and tagged HTTP integration benchmarks, retaining its required disposable-database check. Both use one iteration and verify results; neither enforces timing or allocation thresholds.

Document serial, concurrent, and state-mutating benchmark selectors separately. Use the declared toolchain explicitly and record the actual Go version, build flags, platform, dependency versions, workload dimensions, connection limits, and measurement boundary. Compare ten samples per case. Duration-based evidence uses at least three seconds per sample; fixed-work evidence records its pilot, count, and observed durations.

For example, the portable baseline can start with:

```sh
GOTOOLCHAIN=go1.26.5 go test ./internal/identifier ./internal/dnsname ./internal/httpapi ./internal/httpserver ./internal/upstream \
  -run '^$' -bench '^Benchmark' -benchmem -benchtime=3s -count=10 > before.txt
```

Repeat the same command on the comparison revision into `after.txt`, then run `benchstat before.txt after.txt`. Integration commands must explicitly select read-only duration cases or state-mutating fixed-work cases. Record the benchstat version. Use focused CPU and allocation profiles in separate runs so profiling overhead does not contaminate timing comparisons; database execution evidence requires database-side analysis as well as Go profiles.

Reference: [Go benchmark runners](https://pkg.go.dev/testing#B.RunParallel) and [benchstat sampling guidance](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat). Historical measurements are context, not a comparable baseline unless their conditions match.

Keep raw operational evidence private. Repository evidence uses synthetic identifiers and sanitized environment descriptions and commands; label any redaction and hash the published bytes. No credentials, private machine names, connection strings, or diagnostic machine paths belong in artifacts.

## Risks / Trade-offs

- Synthetic workloads may misrank real bottlenecks → Record their dimensions and measurement boundary; use later production profiles to refine cases before optimizing.
- Database size, plans, caches, or connection setup may dominate results → Use fixed seed populations, explicit warmup, fresh schemas per sample, and identical dependency/configuration choices across revisions.
- Stateful samples can grow without bound or outlive deadlines → Select a fixed work count, check row-count deltas, and provision deadlines for the full sample.
- Loopback HTTP includes client and server work in one process → Label it and retain isolated relay and database results for attribution.
- Concurrent scheduling can alter the synthetic traffic mix → Record operation counts and reject comparisons whose workload mix materially differs.
- More scenarios increase baseline duration → Keep selected cases and separate smoke, duration, fixed-work, and profiling commands.

## Migration Plan

No application deployment or data migration is required. Add test-only fixtures and benchmarks, expand smoke selection, validate correctness, then capture the first expanded baseline in a disposable environment. Reverting the benchmark files, documentation, and target changes restores the previous measurement suite without affecting production behavior.
