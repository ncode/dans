# DANS v1 performance and resource baseline

Captured 2026-08-17 from the `build-dans-v1` working tree before any
performance optimization. The purpose is a measured starting point and broad
anti-bloat budgets, not a claim about production capacity.

The repository base was `7eb786359bf5484f94bac4d513e9b20ec00cff74`; the
greenfield implementation was still a working-tree change, so the executable
hashes below—not that base commit alone—identify the measured artifacts.

## Environment

- Host: Apple M4 (`Mac16,10`), 10 logical CPUs, 16 GiB RAM, macOS 26.5.2.
- Go: `go1.26.5 darwin/arm64`.
- Docker: client/server 29.5.3, Docker Desktop 4.78.0, Linux arm64 server,
  containerd v2.2.4 with overlayfs; Compose v5.1.4.
- Generators: sqlc v1.31.1 and oapi-codegen v2.8.0.
- Go builder: `golang:1.26.5-alpine`, digest
  `sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2`.
- PostgreSQL: `postgres:16.14`, digest
  `sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b`.
- PowerDNS: `powerdns/pdns-auth-51:5.1.3`, digest
  `sha256:f976e753a1de8ec62636203ecb12ae5fa3d1055601be167de53f1f673e0abe59`.
- Toxiproxy: `ghcr.io/shopify/toxiproxy:2.12.0`, digest
  `sha256:9378ed52a28bc50edc1350f936f518f31fa95f0d15917d6eb40b8e376d1a214e`.
- Benchstat: `golang.org/x/perf/cmd/benchstat@v0.0.0-20260615155930-9e4b9ddef5b6`.

## Artifact and runtime results

| Metric | Samples | Median | Range | p95 | Budget |
| --- | ---: | ---: | ---: | ---: | ---: |
| Linux amd64 executable | 1 | 20,459,646 B | - | - | 25,165,824 B |
| Linux arm64 executable | 1 | 19,005,566 B | - | - | 25,165,824 B |
| OCI amd64 packed `.Size` | 1 | 7,286,806 B | - | - | informational |
| OCI arm64 packed `.Size` | 1 | 6,588,886 B | - | - | informational |
| OCI amd64 unpacked rootfs | 1 | 20,709,376 B | - | - | 27,262,976 B |
| OCI arm64 unpacked rootfs | 1 | 19,255,296 B | - | - | 27,262,976 B |
| Offline `version` wall | 50 | 6.399 ms | 6.018-7.969 ms | 7.182 ms | 10 ms median / 15 ms p95 |
| Offline maximum RSS | 50 | 16.004 MiB | 15.914-18.051 MiB | 18.043 MiB | 24 MiB p95 |
| `/bin/true` harness floor | 50 | 0.232 ms | 0.193-0.333 ms | 0.287 ms | informational |
| Start to `/livez` | 15 | 116.927 ms | 107.806-133.191 ms | 133.191 ms | 500 ms p95 |
| Start to `/readyz` | 15 | 122.113 ms | 113.488-137.960 ms | 137.960 ms | 500 ms p95 |
| Ready-idle DANS VmRSS | 5 | 27.375 MiB | 27.375-27.379 MiB | 27.379 MiB | 64 MiB |
| Ready-idle goroutines | 5 | 26 | 26 | 26 | informational |
| Ready-idle OS threads | 5 | 13 | 13 | 13 | informational |
| Ready-idle FDs | 5 | 9 | 9 | 9 | informational |

Idle RSS was 12.355-12.359 MiB anonymous and 15.020 MiB file-backed.
PostgreSQL, PowerDNS, Toxiproxy, and measurement-sidecar memory is excluded.
No `runtime.GC` was invoked.

Release executable hashes:

- amd64: `fe9aa543cf68fc27ac294b68e16179e697369634e1d38a23956fe9cfa7fcc9aa`
- arm64: `a220c4aa83e46d206baadaa6898ff05322abb8676970112fff7b8259884aedea`

Both executables were static, stripped ELF files. The OCI image IDs were
`sha256:61977b1c6f327c657c08b9769da11a2066266fa2838f8054c14e8fd5285c8748`
for amd64 and
`sha256:5e1b520b94bf3a94b858af2792762bdcf50720c689ba580cb81b2da70a8cc49d`
for arm64. All individual measurements and the nearest-rank percentile rule
are in [runtime-samples.txt](runtime-samples.txt).

Docker Desktop's containerd store reports packed bytes through image `.Size`,
while other Docker backends can report unpacked bytes for that field. Packed
sizes are therefore evidence only. CI and release smoke create a stopped
container and gate its unpacked `SizeRootFs` with a broad 26 MiB ceiling:
the 24 MiB executable allowance plus at most 2 MiB of image overhead.

## Representative Go and PostgreSQL benchmarks

| Benchmark | Median | B/op | allocs/op | CV |
| --- | ---: | ---: | ---: | ---: |
| Token validation | 25.77 ns | 32 | 1 | 1.85% |
| Token digest | 51.41 ns | 64 | 1 | 0.13% |
| DNS canonicalization, 1 owner | 366.2 ns | 368 | 17 | 0.16% |
| DNS canonicalization, 100 owners | 36.767 us | 36,800 | 1,700 | 0.48% |
| Strict zone patch, 1 RRset | 7.036 us | 8,480 | 184 | 0.94% |
| Strict zone patch, 100 RRsets | 551.772 us | 471,925 | 13,087 | 3.63% |
| Liveness authentication middleware | 2.989 ns | 0 | 0 | 0.29% |
| Authenticated middleware | 181.5 ns | 528 | 6 | 4.83% |
| PostgreSQL authorization, 1 tuple | 591.976 us | 3,306 | 63 | 1.04% |
| PostgreSQL authorization, 100 tuples | 1.117807 ms | 190,533 | 2,748 | 0.59% |

The five raw runs are preserved in [benchmarks.txt](benchmarks.txt). Benchstat
reported medians but correctly warned that five samples are insufficient for a
95% confidence interval. CV is the sample standard deviation divided by the
arithmetic mean of the five `ns/op` observations. These numbers are therefore a descriptive baseline;
future before/after claims must run both revisions on the same host with enough
samples for the selected confidence level. No path was optimized from this
baseline because none exceeded its budget.

## Reproduction

Canonical release artifacts:

```sh
rtk env GOWORK=off GOFLAGS=-mod=readonly scripts/release.sh build baseline /tmp/dans-release-baseline
rtk scripts/release.sh verify /tmp/dans-release-baseline
```

Canonical OCI artifacts:

```sh
rtk scripts/oci-smoke.sh dans-baseline linux/amd64 baseline
rtk scripts/oci-smoke.sh dans-baseline linux/arm64 baseline
```

Offline Linux startup and maximum RSS (run beside the Linux arm64 release
executable in a disposable native Linux container):

```sh
rtk go build -o /tmp/startup-harness ./openspec/changes/build-dans-v1/evidence/performance/startup-harness.go
rtk proxy /tmp/startup-harness 5 50 /tmp/dans-release-baseline/dans_baseline_linux_arm64 version
```

Repeated native-Linux runtime sampling through the real integration topology:

```sh
rtk env DANS_QA_LOG_DIR=/tmp/dans-integration-logs \
  DANS_QA_PERFORMANCE_DIR=/tmp/dans-performance \
  scripts/integration.sh postgres:16.14
```

Portable benchmark baseline:

```sh
rtk go test ./internal/identifier ./internal/dnsname ./internal/httpapi ./internal/httpserver -run '^$' -bench '^Benchmark' -benchtime=3s -count=5 -benchmem
```

PostgreSQL authorization baseline against a fresh supported database:

```sh
rtk env DANS_TEST_DATABASE_URL='postgres://USER:PASSWORD@HOST/DATABASE?sslmode=disable' go test -tags=integration ./internal/database -run '^$' -bench '^BenchmarkAuthorizeRRsetBatch$' -benchtime=3s -count=5 -benchmem
```

Statistical summary:

```sh
rtk go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260615155930-9e4b9ddef5b6 openspec/changes/build-dans-v1/evidence/performance/benchmarks.txt
```

The exact Docker Desktop baseline build, stack setup, redirections, commands,
nearest-rank estimator, and raw observations are retained in
[runtime-samples.txt](runtime-samples.txt). Its checked
[startup harness](startup-harness.go),
[health harness](health-harness/health-harness.go), and
[goroutine diagnostic](goroutine-sample.sh) are the sources used for the final
run. PostgreSQL and PowerDNS were already healthy; three warmups preceded 15
timed container starts. A PID-namespace sidecar sampled only DANS, and SIGQUIT
was used only on five disposable restarts. Production exposes no performance
or profiling endpoint.
