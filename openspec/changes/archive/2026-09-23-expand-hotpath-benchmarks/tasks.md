## 1. Benchmark fixtures and boundaries

- [x] 1.1 Reuse existing package-local payload, response-writer, and handler fixtures; generalize only shared helpers to `testing.TB`, keep recording state bounded, and identify stubbed versus real dependencies in benchmark names.
- [x] 1.2 Add disposable PostgreSQL fixture support for each leaf benchmark sample, with fixed seed populations, initial/final row counts, production-style pool configuration, and bounded cleanup restricted to the test schema.
- [x] 1.3 Define distinct benchmark selectors for read-only duration cases, durable fixed-work cases, and concurrent cases; preserve existing benchmark names where their measured workload is unchanged.

## 2. Parsing and portable request coverage

- [x] 2.1 Extend DNS benchmarks with Unicode, escaped, and legal long owners; retain 1/100-owner batches and assert canonical output.
- [x] 2.2 Extend strict JSON benchmarks with larger record arrays and late rejection cases, asserting successful decoded content or the exact expected failure category.
- [x] 2.3 Add contract-validation benchmarks for reads and 1/10/100-RRset PATCH bodies, plus near-limit valid bodies, duplicate keys, and unknown fields; verify whether the terminal handler is reached.
- [x] 2.4 Add portable complete-request benchmarks through `NewApplicationHandler` for reads, allowed PATCH batches, and partial denial, retaining real middleware, default request IDs, bounded log output, and labeled database stubs.

## 3. Response forwarding

- [x] 3.1 Add relay benchmarks for 1 KiB/64 KiB/1 MiB responses, success/error statuses, and filtered headers; verify complete bytes and body closure with bounded response state.
- [x] 3.2 Add warmed loopback forwarding cases through the generated client, report payload throughput and connection reuse, and retain the existing deletion benchmark cases.

## 4. Database decision coverage

- [x] 4.1 Add real database authentication benchmarks for valid, unknown, revoked, and expired credentials, asserting the actor or expected authentication error.
- [x] 4.2 Add a real database runtime-compatibility benchmark with valid installation and migration state, excluding fixture setup and connection warmup from timing.
- [x] 4.3 Extend allowed authorization cases with direct/group grants, exact/glob selectors, overlaps, nonmatching candidates, and 1/100/1000-grant populations using selected independent dimensions rather than a Cartesian product.
- [x] 4.4 Add separately selectable partial/full-denial benchmarks that assert denied tuples, no false matches, and committed denial events; use fixed-work comparison samples.

## 5. Audit and complete PostgreSQL requests

- [x] 5.1 Add committed intent/outcome benchmarks for 1/100 RRsets and success/failure outcomes; verify event links, row-count deltas, and valid deadlines without replacing commits with rollback-only measurements.
- [x] 5.2 Assemble tagged HTTP integration benchmarks with real authentication, compatibility checks, request stores, and production-style pools, using a controlled loopback upstream and test-only fixture code.
- [x] 5.3 Add complete authenticated read benchmarks and separately selectable allowed/denied PATCH benchmarks; assert response content, upstream call counts, and durable audit side effects.

## 6. Concurrent requests

- [x] 6.1 Add `RunParallel` cases with worker-local request/response state, bounded shared pools, fixed request/concurrency limits, and recorded `GOMAXPROCS` and worker counts.
- [x] 6.2 Cover reads, writes, and a synthetic 9-read/1-write sequence; report actual operation counts, preserve the first unexpected failure, and apply fixed-work sampling to write and mixed cases.
- [x] 6.3 Verify workers return and transports, pools, and servers close on success and request error/timeout, including a short race-enabled execution of the benchmark bodies.

## 7. CI and reproduction commands

- [x] 7.1 Expand `benchmark-check` to include upstream and portable complete-request cases; expand `benchmark-postgres-check` to select all database and tagged HTTP integration cases while retaining its missing-database failure.
- [x] 7.2 Document exact portable, PostgreSQL read-only, fixed-work, and parallel commands with the declared toolchain, ten samples, at least three seconds for duration cases, and a recorded pilot/count for state-mutating cases.
- [x] 7.3 Document same-environment `benchstat` comparison and separate focused CPU/allocation profiling commands, including dependency versions, fixture sizes, limits, and measurement boundaries.

## 8. Validation and initial evidence

- [x] 8.1 Run affected-package correctness tests, both one-iteration benchmark smoke targets, and the missing-database diagnostic check; verify that transient unexpected failures cannot be masked by a later successful iteration.
- [x] 8.2 Run affected integration correctness tests and focused race-enabled benchmark smokes against a disposable supported PostgreSQL database; keep race/profiling results separate from performance timing.
- [x] 8.3 Capture the expanded baseline with ten comparable samples per case, validate fixed-work row-count deltas and effective mixed-traffic counts, and summarize measurement variation without claiming production capacity or optimization wins.
- [x] 8.4 Inspect exact evidence artifacts for private identifiers before adding them to the repository; retain raw operational output privately and include only sanitized summaries and reproducible commands, labeling any redactions.
- [x] 8.5 Validate the completed change against its specification and confirm there are no production behavior, API, schema migration, generated code, runtime dependency, or profiling-route changes.
