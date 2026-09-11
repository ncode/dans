# Expanded benchmark baseline

Captured on 2026-09-09: **73 cases, ten valid samples each (730 measurements)**.
These are synthetic reference measurements. Use fresh paired runs for an
optimization claim; the collection does not estimate deployed-system capacity.

- [benchmarks.txt](benchmarks.txt): sanitized measurement rows, including all custom metrics.
- [benchstat.txt](benchstat.txt): medians and 95% confidence intervals for time and allocations; `-` denotes standard input.
- [summary.csv](summary.csv): per-case medians, sample coefficient of variation, and measured-duration ranges.
- [pilot.txt](pilot.txt): the twelve fixed-work pilot measurements.
- [Runbook](../../../../../docs/benchmarks.md): fixture boundaries, routine reproduction, profiling, and race checks.

## Environment and source identity

Go `go1.26.5 darwin/arm64`, `-cpu=4`, no race detector or profiling during
timing, and serial package execution with `-p=1` for multi-package commands.
Measurements used a shared development environment; timing dispersion is
reported below. PostgreSQL was version 16.14 with `fsync=on` and
`synchronous_commit=on`. Its server CPU and memory are outside Go's allocation
measurements.

The database driver was pgx v5.10.0, contract validation used kin-openapi
v0.142.0 and nethttp-middleware v1.2.0, and generated-client runtime support was
v1.6.0. The unchanged dependency manifests identify the remaining versions:

- `go.mod` SHA-256: `34368f8d12d321c1fd5076bd7b10294a7c7a69536d7be208177ee4f051134509`
- `go.sum` SHA-256: `add86f7f3ed58d7fa5cd5ff672a16e2dd200bc91d9971e460e0fa644bf0dc159`

Collection began at base commit `48c65fe` plus the benchmark changes. Two
existing benchmark bodies subsequently gained immediate failure assertions:
token validation and authentication middleware. Their three measurement cases
were recollected, replacing the earlier rows. No production source changed
during collection; the other measured bodies are unchanged.

The two source fingerprints below cover 148 files: sorted, unique Git-listed
tracked and non-ignored untracked files under `api` and `internal`, plus
`go.mod`, `go.sum`, and `sqlc.yaml`. SHA-256 consumes each relative filename,
a NUL byte, its bytes, and another NUL byte. Removing only the two assertion
insertions reconstructs the initial fingerprint.

- Initial: `35b5143829557e25ecad1c663f9b07ab982fa359873817036f214dfd0dff5f0a`
- Final: `495b20f6b7aeafabe9a27fd2cf2265f1bc75ad311ea9aaf0eea3471f61ca4c9a`

## Collection commands and corrections

Configure the repository's disposable test-database setting privately. Commands
run from the repository root; output paths below replace the original private
storage paths. The original portable collection used ten consecutive samples
per leaf:

```sh
BENCHMARK_OUTPUT_DIR="$(mktemp -d)"
GOTOOLCHAIN=go1.26.5 go test -p=1 ./internal/identifier ./internal/dnsname ./internal/httpapi ./internal/httpserver ./internal/upstream \
  -run '^$' -bench '^Benchmark' -benchmem -benchtime=3s -count=10 -cpu=4 > "$BENCHMARK_OUTPUT_DIR/portable.txt" 2>&1
GOTOOLCHAIN=go1.26.5 go test -p=1 -tags=integration ./internal/database ./internal/httpserver \
  -run '^$' -bench '^Benchmark(AuthorizeRRsetBatch|AuthenticatePostgres|RuntimeCompatibilityPostgres|AuthorizationGrants|ApplicationPostgresRead|ApplicationPostgresParallelRead)$' \
  -benchmem -benchtime=3s -count=10 -cpu=4 > "$BENCHMARK_OUTPUT_DIR/postgres-read.txt" 2>&1
GOTOOLCHAIN=go1.26.5 go test -p=1 -tags=integration ./internal/database ./internal/httpserver \
  -run '^$' -bench '^Benchmark(AuthorizationDeniedWrite|DNSAuditWrite|ApplicationPostgresWrite|ApplicationPostgresParallelWrite)$' \
  -benchmem -benchtime=2000x -count=10 -cpu=4 > "$BENCHMARK_OUTPUT_DIR/postgres-write.txt" 2>&1
```

The fixed-work pilot used the last command with `-count=1`. Each sample created
a fresh schema and seed population; audit state began empty. Real commits,
request processing, and response assertions were timed. Setup, warmup, state
verification, and cleanup were excluded. The runbook records input sizes,
logging/body-reset overhead, pool warmup, and connection limits.

Consecutive delayed deletion-retry samples exhausted local ephemeral TCP ports.
Temporary HTTP tracing confirmed an address-unavailable dial error, and a TCP
state count confirmed exhaustion. The tracing was removed. The original run
contained failure diagnostics despite a successful process exit, so acceptance
checks inspected diagnostics and measurement counts independently.

All original rows for that case were replaced with ten isolated samples using
35 seconds of untimed cooldown between samples. All ten passed, retaining the
expected deletion result and approximately one new connection per operation.
The runbook now repeats the whole portable suite between samples to provide
natural separation for this connection-churn case.

```sh
GOTOOLCHAIN=go1.26.5 go test -p=1 ./internal/identifier ./internal/httpserver \
  -run '^$' -bench '^Benchmark(ValidateToken|AuthenticationMiddleware)$' \
  -benchmem -benchtime=3s -count=10 -cpu=4 > "$BENCHMARK_OUTPUT_DIR/guarded.txt" 2>&1
for sample in 1 2 3 4 5 6 7 8 9 10; do
  GOTOOLCHAIN=go1.26.5 go test ./internal/httpserver -run '^$' \
    -bench '^BenchmarkZoneDeletion$/^Retry$/^NotFound4KiBDelayed10ms$' \
    -benchmem -benchtime=3s -count=1 -cpu=4 || exit
  if [ "$sample" -lt 10 ]; then sleep 35; fi
done > "$BENCHMARK_OUTPUT_DIR/retry-cooled.txt" 2>&1
```

All accepted duration samples contain at least three seconds of measured work.
The raw originals, failed measurements, and diagnostic output remain private.

## Fixed work and variation

Every durable sample used 2,000 operations. Durations below are seconds,
calculated from the reported iteration count and `ns/op`. Baseline durations
were substantially longer than the pilot for several write cases; the operation
count and seed state remained fixed. This drift reinforces the need for fresh
paired comparisons.

| Case | Pilot seconds | Ten-sample seconds range | Audit rows |
| --- | ---: | ---: | --- |
| AuthorizationDeniedWrite/1/Partial=false | 1.63 | 6.80–7.72 | 0 → 2000 |
| AuthorizationDeniedWrite/100/Partial=false | 3.05 | 11.72–13.32 | 0 → 2000 |
| AuthorizationDeniedWrite/100/Partial=true | 2.70 | 10.99–12.78 | 0 → 2000 |
| DNSAuditWrite/1/succeeded | 2.76 | 11.41–12.89 | 0 → 4000 |
| DNSAuditWrite/1/failed | 2.82 | 10.17–13.03 | 0 → 4000 |
| DNSAuditWrite/100/succeeded | 3.54 | 14.42–23.77 | 0 → 4000 |
| DNSAuditWrite/100/failed | 3.75 | 14.37–15.75 | 0 → 4000 |
| ApplicationPostgresWrite/1 | 4.80 | 18.97–22.92 | 0 → 4000 |
| ApplicationPostgresWrite/100 | 8.38 | 32.15–36.32 | 0 → 4000 |
| ApplicationPostgresWrite/Denied | 2.24 | 11.45–12.34 | 0 → 2000 |
| ApplicationPostgresParallelWrite/Write | 2.21 | 7.59–8.43 | 0 → 4000 |
| ApplicationPostgresParallelWrite/Mixed | 0.65 | 2.18–2.44 | 0 → 398/400 |

Concurrent samples used four workers, `GOMAXPROCS=4`, 16 total pool connections
(4 authentication, 1 readiness, 11 requests), request limit 32, and five-second
request/statement bounds. Mixed samples contained 1,800–1,801 reads and 199–200
writes: a 9.95–10.00% write share, a 0.05 percentage-point spread. Their 398–400
audit rows matched twice the actual write count. Concurrent `ns/op` expresses
aggregate throughput, not a latency percentile.

Nine of 73 cases had a time coefficient of variation above 10%. CV uses sample
standard deviation divided by the arithmetic mean; it is distinct from the
median confidence intervals in the benchstat output. All fifteen read-only
database cases had CV below 5%. No timing threshold was added to CI.

| Case with CV above 10% | Time CV |
| --- | ---: |
| CanonicalizeOwnerVariants/Long-4 | 41.11% |
| ApplicationStubbed/Patch1-4 | 31.72% |
| ForwardLoopback/1024/Status=502-4 | 29.92% |
| ApplicationStubbed/Patch10-4 | 24.09% |
| ForwardLoopback/65536/Status=200-4 | 23.77% |
| ForwardLoopback/1024/Status=200-4 | 19.34% |
| DNSAuditWrite/100/succeeded-4 | 18.59% |
| Relay/1024/Filtered=false-4 | 17.32% |
| AuthenticationMiddleware/authenticated-4 | 10.56% |

Benchstat was pinned to
`v0.0.0-20260615155930-9e4b9ddef5b6` and executed from its cached module source
offline with Go 1.26.5. The equivalent versioned invocation is:

```sh
go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260615155930-9e4b9ddef5b6 \
  -filter '.unit:(ns/op OR B/op OR allocs/op)' - < benchmarks.txt
```

## Validation and redaction

Affected integration correctness tests, both benchmark smoke targets, and the
focused race-enabled parallel benchmarks/cancellation check passed. The final
smokes exercised all 73 cases, plus three parallel cases under the race
detector. Earlier checks also covered the missing-database diagnostic, URI and
keyword database configuration, and a temporary first-iteration failure that
remained detectable after later successful parallel operations. Fault injection
and transport tracing were removed.

**Sanitized evidence:** private paths, module-identifying headers, and raw
diagnostics were omitted. Measurement whitespace was normalized; retained
numeric fields were preserved. Superseded and failed samples are excluded.
These SHA-256 values describe the actual shared bytes:

- `benchmarks.txt`: `927df74f4d8528c5b2fd3d5c1f69824f6273ed0812f652c92a468699dca0a9e0`
- `summary.csv`: `b56b5cb7b6e6f6d66325f39f09e1c7cf778d8d1b9aa1c197735db81499137c91`
- `pilot.txt`: `29feefc5665ddebbc809caef02a5995a84dd18f35eb4008ec2a9348c3f052287`
- `benchstat.txt`: `5f5ac32d4fbe1f3ab0aa2adab33e84cb8dfad512fe5fd735664f1d486b69e32f`
