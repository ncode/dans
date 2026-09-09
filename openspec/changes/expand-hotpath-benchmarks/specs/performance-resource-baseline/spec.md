## MODIFIED Requirements

### Requirement: Benchmarks exercise real supported paths
The service SHALL retain benchmarks for token validation and digesting, DNS owner canonicalization, strict zone-patch decoding, authentication middleware, zone deletion, and PostgreSQL-backed RRset batch authorization. Batch-sensitive benchmarks MUST cover one item and the supported maximum of 100 items. Database benchmarks MUST use a real supported PostgreSQL instance with representative persisted identity, token, zone-binding, delegation, selector, and relevant audit state rather than a mock.

The suite SHALL additionally measure complete authenticated reads and RRset writes, per-request database authentication and compatibility checks, durable intent and outcome recording, response forwarding, and bounded concurrent requests. Input variants MUST cover Unicode and escaped DNS owners, larger record arrays, late JSON rejection, small and large response bodies, and success and denial paths. Authorization variants MUST include direct and group grants, exact and glob selectors, overlapping grants, nonmatching candidates, and increasing grant populations.

Every benchmark MUST use fixed workload inputs prepared outside measurement, report allocations, exclude fixture setup and cleanup from reported timing and allocation measurements, and validate the result after measured work completes. Serial and concurrent benchmark runners are both permitted. Transient unexpected errors MUST fail a benchmark even when a later operation succeeds. Workload sizes MUST be independent of automatically selected iteration counts.

#### Scenario: Portable benchmark smoke runs
- **WHEN** the normal benchmark check runs without external services
- **THEN** token, DNS, strict-JSON, middleware, deletion, forwarding, and portable complete-request cases compile, execute their declared paths, report allocations, and validate their results using only local fixtures and loopback HTTP where required

#### Scenario: Authorization benchmark runs
- **WHEN** the integration benchmark is given a fresh supported PostgreSQL database
- **THEN** it measures the production authorization statement for one and 100 RRset tuples against seeded relational state
- **AND** selected variants exercise grant types, grant populations, overlapping matches, and partially and fully denied batches

#### Scenario: Shared database checks are measured
- **WHEN** database dependency benchmarks run
- **THEN** credential lookup and runtime compatibility are measured against persisted state, including accepted and rejected credentials
- **AND** accepted credentials return the expected actor while rejected credentials produce the expected authentication failure

#### Scenario: Mutation persistence is measured
- **WHEN** audit or real-database write benchmarks run
- **THEN** measurements include durable intent and outcome commits or a committed authorization denial as appropriate
- **AND** the benchmark verifies the expected event counts and relationships

#### Scenario: A benchmark input becomes invalid
- **WHEN** a benchmark no longer produces the expected canonical value, decoded batch, authenticated response, authorization decision, forwarded response, or audit side effect
- **THEN** the benchmark fails instead of publishing misleading timing data

### Requirement: Benchmark evidence is statistically reproducible
The verified expanded baseline SHALL preserve raw benchmark output from at least ten runs per benchmark with memory allocation reporting and a statistical summary. Read-only and resettable portable cases MUST use at least three seconds of measured work per sample. Cases that append durable state MUST use an explicitly fixed operation count selected by a recorded pilot, the same initial fixture state for every sample, and recorded measured durations and final row counts. The selected operation count MUST remain identical across revisions being compared.

Performance comparisons MUST use the same host, toolchain, build flags, input sizes, dependency versions, benchmark command, warmup policy, and connection and concurrency limits for both revisions. A statistically supported regression MUST be investigated and documented before replacing the accepted baseline. Historical evidence collected under different conditions MUST NOT be treated as a comparable before measurement.

Absolute `ns/op` or allocation values from a shared runner MUST NOT become hard CI limits. CI SHALL instead run short portable and real-PostgreSQL benchmark correctness smokes and enforce the artifact and root-filesystem ceilings; repeated timing and allocation comparisons remain evidence-driven. The PostgreSQL smoke entry point MUST fail clearly when no disposable test database is configured, rather than report success with all integration benchmarks skipped.

Raw operational evidence MUST remain private. Shared evidence MUST use synthetic identifiers and sanitized environment descriptions and reproduction commands, with redactions labeled and any published hashes computed from the published bytes. Credentials, private machine identifiers, connection strings, and diagnostic machine paths MUST NOT be included in shared artifacts.

#### Scenario: Baseline is captured
- **WHEN** an expanded performance baseline is accepted
- **THEN** repeated benchmark output and its statistical summary are retained with the reproduction command, environment, measurement boundary, and workload dimensions
- **AND** duration-based and fixed-work samples follow their respective repetition and fixture rules

#### Scenario: Optimization is proposed
- **WHEN** a code change claims lower latency, CPU use, or allocations
- **THEN** the claim is supported by same-host before-and-after repeated measurements rather than a single run
- **AND** correctness and relevant race checks pass independently of timing measurements

#### Scenario: Shared CI timing varies
- **WHEN** a shared CI runner reports different absolute benchmark timings while correctness and deterministic footprint budgets still pass
- **THEN** CI does not reject the change solely because of that isolated timing value

#### Scenario: Integration smoke lacks its dependency
- **WHEN** the PostgreSQL benchmark smoke entry point runs without a configured disposable test database
- **THEN** it exits unsuccessfully with a configuration diagnostic that does not disclose credentials

#### Scenario: Evidence is shared
- **WHEN** benchmark evidence is prepared for a repository, report, or upload
- **THEN** only sanitized evidence is included, any redaction is labeled, and the private original is retained separately

## ADDED Requirements

### Requirement: Measurement boundaries are explicit
Each benchmark result MUST identify whether it measures isolated Go work, real database operations, or a complete request. Complete-request benchmarks MUST execute production middleware and routing, including request limits, contract validation, authentication, compatibility checks, authorization, and log formatting as applicable. Stubbed dependencies and loopback upstreams MUST be identified so their measurements cannot be mistaken for deployed-system capacity.

Complete-request variants MUST include both controlled database dependencies and real PostgreSQL, with an authenticated read and allowed and denied RRset writes. Forwarding benchmarks MUST verify complete response bytes, status, credential/header filtering, and response-body closure, and SHALL include a warm-connection transport case. Byte-throughput results MUST state the payload size. Reported Go allocations MUST NOT be described as database-server memory usage.

#### Scenario: Portable complete request runs
- **WHEN** a complete request is benchmarked with controlled database dependencies
- **THEN** the production HTTP path executes, dependency stubs are identified, and expected response and forwarding behavior are verified

#### Scenario: PostgreSQL complete request runs
- **WHEN** a complete request is benchmarked with PostgreSQL
- **THEN** authentication, compatibility, authorization, and applicable audit persistence use real database operations
- **AND** a denied write is verified not to reach the upstream

#### Scenario: Large response is relayed
- **WHEN** a forwarding benchmark uses a large success or error response
- **THEN** it verifies all expected bytes and headers, consumes and closes the response, and records connection reuse for the transport case

### Requirement: Stateful and concurrent benchmarks preserve workload integrity
State-mutating comparison samples MUST start from a fresh disposable fixture for each leaf benchmark sample, retain real commits, and verify initial and final state. Fixture cleanup MUST run outside measurement and MUST NOT affect any non-test schema. Rolling back all measured writes in an outer transaction MUST NOT substitute for measuring committed persistence. Duration-only runs of these cases SHALL serve as correctness smokes rather than accepted comparison evidence.

Concurrent cases MUST use bounded shared database connections with independent request bodies and response state for each worker. They SHALL include reads, writes, and a declared fixed mixed workload. Reported evidence MUST include worker count, effective operation mix, connection and request limits, and the same settings across compared revisions. All workers and owned resources MUST finish or close within bounded cleanup before the benchmark completes. Concurrent throughput results MUST NOT be represented as per-request latency percentiles.

#### Scenario: Durable samples are repeated
- **WHEN** a state-mutating benchmark is repeated or compared across revisions
- **THEN** each sample begins with the same seed population, executes the same selected operation count, and validates the expected state delta independently of earlier samples

#### Scenario: Concurrent requests finish
- **WHEN** a concurrent benchmark finishes or a request encounters an error or timeout
- **THEN** every worker returns, owned resources are closed, and any unexpected operation failure makes the benchmark fail
- **AND** a short race-enabled execution checks the concurrent benchmark path separately from performance timing

#### Scenario: Mixed workloads are compared
- **WHEN** concurrent mixed-traffic results are compared
- **THEN** both revisions use the same declared sequence and concurrency limits, report actual read/write counts, and are accepted for comparison only when their effective workloads are comparable
