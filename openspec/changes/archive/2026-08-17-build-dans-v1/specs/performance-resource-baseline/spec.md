## Purpose

Defines reproducible performance and resource measurements that keep DANS release artifacts and its steady-state control-plane runtime within explicit, evidence-backed budgets.

## ADDED Requirements

### Requirement: Release artifact footprint is bounded
DANS SHALL build its canonical CGO-free, stripped Linux amd64 and arm64 release executables from the pinned toolchain, and each executable MUST be no larger than 24 MiB (25,165,824 bytes). A stopped container created from each final OCI image MUST report an unpacked `SizeRootFs` no larger than 26 MiB (27,262,976 bytes), allowing at most 2 MiB beyond the executable ceiling for certificates and image files. Release and continuous-integration verification MUST fail when either ceiling is exceeded.

#### Scenario: Release artifacts remain within budget
- **WHEN** the canonical release recipe builds both supported architectures and their OCI images
- **THEN** each executable and unpacked container root filesystem is measured in bytes and passes its respective 24 MiB and 26 MiB ceiling

#### Scenario: Executable footprint regresses
- **WHEN** either canonical release executable exceeds 25,165,824 bytes
- **THEN** release verification fails before the artifact can be accepted

#### Scenario: Image footprint regresses
- **WHEN** a stopped container from either canonical OCI image reports a `SizeRootFs` greater than 27,262,976 bytes
- **THEN** image verification fails before the image can be accepted

### Requirement: Ready-idle runtime resources are bounded
A disposable production DANS instance connected to healthy pinned PostgreSQL and PowerDNS dependencies SHALL be measured after readiness and before test workload. Across at least five samples, the DANS process MUST remain at or below 64 MiB VmRSS. Measurements MUST exclude PostgreSQL, PowerDNS, network-fault tools, and measurement-sidecar resources. Goroutine, operating-system-thread, and file-descriptor counts SHALL be recorded as diagnostic evidence but MUST NOT require a production introspection endpoint or destructive CI probe.

#### Scenario: Ready instance remains lightweight
- **WHEN** a production instance reaches readiness and at least five ready-idle samples are collected before workload
- **THEN** each DANS process sample uses no more than 64 MiB VmRSS and diagnostic concurrency counts are recorded separately

#### Scenario: Dependency resources are measured separately
- **WHEN** the runtime baseline is collected from the real-system stack
- **THEN** the reported DANS totals exclude every dependency and measurement process

#### Scenario: Idle resource budget is exceeded
- **WHEN** a measured ready-idle DANS process exceeds 64 MiB VmRSS
- **THEN** the baseline fails and the release cannot claim the verified lightweight footprint

### Requirement: Startup and readiness have repeated budgets
The canonical offline `version` command SHALL be measured after warmup for at least 50 executions and MUST have a median wall time no greater than 10 ms, a p95 wall time no greater than 15 ms, and a p95 maximum resident set no greater than 24 MiB on the pinned baseline runner. With PostgreSQL and PowerDNS already healthy, at least 15 fresh DANS starts MUST have p95 time-to-liveness and p95 time-to-readiness no greater than 500 ms.

These timing budgets MUST be evaluated on a pinned runner using the same source, toolchain, inputs, and measurement harness. Shared-runner timing MUST NOT be used as a single-sample hard gate.

#### Scenario: Offline startup is measured
- **WHEN** the release executable completes five warmups followed by 50 measured `version` invocations
- **THEN** the recorded median, p95, range, and maximum-RSS p95 satisfy the offline budgets

#### Scenario: Warm-dependency startup is measured
- **WHEN** PostgreSQL and PowerDNS are already healthy and 15 fresh DANS containers are started and polled without fixed sleeps
- **THEN** the recorded liveness and readiness p95 values are each no greater than 500 ms

#### Scenario: Noisy timing sample changes
- **WHEN** timing is compared across source revisions
- **THEN** both revisions are measured on the same pinned runner with the same harness and repeated sample counts rather than compared as isolated CI observations

### Requirement: Benchmarks exercise real supported paths
DANS SHALL retain a minimal benchmark suite for token validation and digesting, DNS owner canonicalization, strict PowerDNS zone-patch decoding, authentication middleware, and the PostgreSQL-backed RRset batch-authorization statement. Batch-sensitive benchmarks MUST cover one item and the supported maximum of 100 items. The SQL benchmark MUST use a real supported PostgreSQL instance with representative persisted identity, token, zone-binding, delegation, and selector state rather than a mock.

Every Go benchmark MUST use `testing.B.Loop`, fixed inputs prepared outside the measured loop, `ReportAllocs`, and a correctness assertion after the loop.

#### Scenario: Portable benchmark smoke runs
- **WHEN** the normal benchmark check runs without external services
- **THEN** token, DNS, strict-JSON, and middleware benchmarks compile, execute their real paths, report allocations, and validate their results

#### Scenario: Authorization benchmark runs
- **WHEN** the integration benchmark is given a fresh supported PostgreSQL database
- **THEN** it measures the production authorization statement for one and 100 RRset tuples against seeded relational state

#### Scenario: A benchmark input becomes invalid
- **WHEN** a benchmark no longer produces the expected canonical value, decoded batch, authenticated response, or authorization decision
- **THEN** the benchmark fails instead of publishing misleading timing data

### Requirement: Benchmark evidence is statistically reproducible
The verified baseline SHALL preserve raw benchmark output from at least five runs of at least three seconds per benchmark with memory allocation reporting, plus a statistical summary. Performance comparisons MUST use the same host, toolchain, build flags, input sizes, dependency versions, and benchmark command for both revisions. A statistically supported regression MUST be investigated and documented before replacing the accepted baseline.

Absolute `ns/op` or allocation values from a shared runner MUST NOT become hard CI limits. CI SHALL instead run short portable and real-PostgreSQL benchmark correctness smokes and enforce the artifact and root-filesystem ceilings; repeated timing and allocation comparisons remain evidence-driven.

#### Scenario: Baseline is captured
- **WHEN** a release performance baseline is accepted
- **THEN** raw repeated benchmark output and its statistical summary are stored with the exact reproduction command and environment

#### Scenario: Optimization is proposed
- **WHEN** a code change claims lower latency, CPU use, or allocations
- **THEN** the claim is supported by same-host before-and-after repeated measurements rather than a single run

#### Scenario: Shared CI timing varies
- **WHEN** a shared CI runner reports different absolute benchmark timings while correctness and deterministic footprint budgets still pass
- **THEN** CI does not reject the change solely because of that isolated timing value

### Requirement: Measurement remains outside the production API
The baseline SHALL record the source revision, UTC date, host and container platform, Go and generator versions, exact commands, sample counts, medians, p95 values, ranges, artifact hashes, and dependency image digests needed to reproduce it. Measurement tooling MUST operate through release artifacts, operating-system process data, public health endpoints, and disposable test infrastructure; it MUST NOT add a production metrics, profiling, test-control, or introspection endpoint.

#### Scenario: Baseline can be audited
- **WHEN** an operator reads the checked-in performance evidence
- **THEN** the environment, inputs, commands, raw samples, summaries, budgets, and artifact identities are available without relying on an unrecorded local state

#### Scenario: Production routes are inspected
- **WHEN** the production OpenAPI and HTTP route set are compared before and after adding the baseline
- **THEN** no metrics, profiling, benchmark, or test-only introspection route has been added
