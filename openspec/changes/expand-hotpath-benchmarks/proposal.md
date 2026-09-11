## Why

The existing benchmarks measure several primitives and isolated handlers, but do not measure complete requests or the database compatibility and durable audit work on their paths. Broader, reproducible coverage is needed to identify actual bottlenecks before proposing optimizations.

## What Changes

- Add complete authenticated read and RRset write benchmarks through the production middleware and router, with explicit measurement boundaries for controlled dependencies and real PostgreSQL.
- Measure database authentication, per-request compatibility checks, authorization with realistic grant populations, and committed audit writes.
- Extend DNS and JSON input variants and add response relay benchmarks for body sizes, headers, and connection reuse.
- Add bounded concurrent request benchmarks using production pool configuration and independent worker state.
- Retain correctness checks and allocation reporting; distinguish serial `b.Loop` benchmarks from concurrent `RunParallel` benchmarks.
- Expand short CI benchmark checks and document repeated baseline, comparison, and profiling commands. Keep timing comparisons outside hard shared-runner gates.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `performance-resource-baseline`: Expand supported-path coverage, define concurrent and stateful measurement rules, and strengthen repeatable benchmark evidence.

## Impact

- Benchmark files and reusable test fixtures in `internal/httpserver`, `internal/database`, `internal/httpapi`, `internal/dnsname`, and `internal/upstream`.
- Benchmark targets in `Makefile`, their CI invocation, and benchmark reproduction documentation.
- Disposable PostgreSQL schemas and loopback HTTP servers for integration measurements.
- No production behavior, public API, schema migration, generated code, or runtime dependency changes. Optimization remains a separate change justified by the resulting evidence.
