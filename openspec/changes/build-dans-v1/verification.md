# Verified release baseline

This records the release-candidate verification for OpenSpec change
`build-dans-v1` on 2026-08-14. Every command below completed successfully on
the same frozen source tree after final review found no open implementation
issues.

## Toolchain and pinned dependencies

- Go: `go1.26.5 darwin/arm64`
- sqlc: `v1.31.1`
- oapi-codegen: `v2.8.0`
- OpenSpec CLI: `1.9.0`
- PowerDNS API/runtime: `powerdns/pdns-auth-51:5.1.3`
- Vendored PowerDNS OpenAPI SHA-256:
  `9b224e5b2456648503efd367bf9724325bb075337dd18ecadd31df6792b8d636`

## Generated code, contracts, and Go checks

| Command | Result |
| --- | --- |
| `make generate-check` | OpenAPI and sqlc generation matched the checked-in output. |
| `go test ./... -count=1` | 462 tests passed across 14 packages. |
| `go test -race ./... -count=1` | 462 tests passed across 14 packages with the race detector. |
| `go vet ./...` | Clean. |
| `go mod verify` | All modules verified. |
| `go tool sqlc vet -f sqlc.yaml` | Clean. |
| `sh -n scripts/integration.sh` | Integration harness syntax valid. |
| `scripts/integration-contract.sh` | Integration and CI contract passed. |
| `scripts/release_workflow_test.sh` | Release-workflow contract passed. |
| `openspec validate build-dans-v1 --strict` | Change valid in strict mode. |
| `git diff --check` | Clean. |

## Real-system matrix

Each run rebuilt the production DANS image, created a fresh isolated Compose
project and database, ran two DANS instances, and used PowerDNS 5.1.3. The
matrix exercised compiled-CLI bootstrap and migrations, forward and PTR DNS,
all RRset change kinds, direct and group authority, cross-instance revocation,
metadata fidelity, denials, audit and zone lifecycle, PostgreSQL and PowerDNS
faults, response ambiguity, audit failure, and schema incompatibility. It also
proved that a never-bound PowerDNS zone can be deleted while a repeated raw
delete for retired binding history returns 409 without forwarding or creating
another intent.

| PostgreSQL | Command | Result |
| --- | --- | --- |
| 16.14 | `env DANS_QA_LOG_DIR=/private/tmp/dans-qa-freeze2-pg16 scripts/integration.sh postgres:16.14` | `integration: PostgreSQL 16.14 passed` |
| 18.4 | `env DANS_QA_LOG_DIR=/private/tmp/dans-qa-freeze2-pg18 scripts/integration.sh postgres:18.4` | `integration: PostgreSQL 18.4 passed` |

## Release and deployment artifacts

| Command | Result |
| --- | --- |
| `scripts/release_test.sh` | Reproducible Linux amd64/arm64 executables, matching checksums, and tamper rejection passed. |
| `scripts/deploy-smoke.sh` | Docker Compose and Kubernetes examples validated. |
| `scripts/oci-smoke.sh dans-oci-freeze linux/arm64 v0.0.0-freeze` | Passed; image reports `linux/arm64`, runs as `65532:65532`, and exposes the expected version/help/migration command surface. |
| `scripts/oci-smoke.sh dans-oci-freeze linux/amd64 v0.0.0-freeze` | Passed; image reports `linux/amd64`, runs as `65532:65532`, and exposes the expected version/help/migration command surface. |

Both OCI images use entrypoint `["/usr/local/bin/dans"]` and default command
`["serve"]`. The local exploratory artifacts `dans`, `ogen.tar.gz`, and
`ogen-1.24.0/` are not release inputs and must remain excluded from commits and
published artifacts.

## 2026-08-17 generated-code and footprint addendum

The generated-client review findings were remediated at their source rather
than by editing generated files:

- the supported public client installs a 30-second default HTTP client and a
  64-MiB response-body cap after caller options, including a safe nil-client
  fallback;
- nullable PostgreSQL UUIDs generate consistently as `*string` through the
  checked-in sqlc configuration;
- audit terminal outcomes use `succeeded`, `failed`, and `unknown` consistently;
- the overdue-intent closer rejects a mismatched `row_limit`/event-ID batch as
  an atomic no-op before sqlc regeneration. A real PostgreSQL 16.14 regression
  proves an exact retry closes the complete batch.

OpenCodeReview `v1.3.19` reviewed the final generated-code payload after the
user explicitly approved its external upload. The local preview confirmed that
the payload contained exactly `api/openapi.gen.go`, `internal/database/db.go`,
`internal/database/models.go`, and `internal/database/resources.sql.go`
(`+27,479` generated lines). The review returned one comment suggesting that
operator RRset writes require an active binding. It was rejected as a false
positive: the accepted gateway and delegation contracts explicitly grant
operators every compatible operation and RRset without a delegation, the
production caller intentionally audits an unbound operator target as a
`powerdns_zone`, and the real-PostgreSQL authorization suite proves both the
unbound operator bypass and its immediate removal after demotion. No generated
or generator-source change was warranted.

### Current correctness baseline

| Command | Result |
| --- | --- |
| `make generate-check` | OpenAPI and sqlc generation matched checked-in output. |
| `go test ./... -count=1` | 468 tests passed across 16 packages. |
| `go test -race ./... -count=1` | 468 tests passed across 16 packages with the race detector. |
| `go vet ./...` | Clean. |
| `go mod verify` | All modules verified. |
| `go tool sqlc vet -f sqlc.yaml` | Clean. |
| `make benchmark-check` | Portable benchmark correctness cases ran. |
| `make benchmark-postgres-check` | Real PostgreSQL 16.14 authorization benchmark cases ran. |
| `scripts/release_test.sh` | Reproducibility, checksums, tamper rejection, and the 24-MiB executable ceiling passed. |
| `make integration-contract` | CI, unpacked-rootfs, native-Linux RSS, and real-system harness contracts passed. |
| `openspec validate build-dans-v1 --strict --no-interactive` | Change valid in strict mode. |
| `git diff --check` | Clean. |

The current real-system smoke rebuilt the production image, enforced its
19,255,296-byte unpacked root filesystem, and passed the complete two-instance
workflow against PostgreSQL 16.14 and 18.4 with PowerDNS 5.1.3:

```text
scripts/integration.sh postgres:16.14
integration: PostgreSQL 16.14 passed
scripts/integration.sh postgres:18.4
integration: PostgreSQL 18.4 passed
```

The native-Linux CI path reads each ready DANS process's `VmRSS` and fails over
64 MiB; Docker Desktop reports the gate as explicitly deferred rather than
silently pretending to expose the daemon VM's host `/proc`.

### Measured performance and resources

The complete environment, raw samples, commands, hashes, and benchmark results
are in [evidence/performance/README.md](evidence/performance/README.md). The
accepted measurements are:

| Metric | Baseline | Budget |
| --- | ---: | ---: |
| Linux amd64 / arm64 executable | 20,459,646 B / 19,005,566 B | 25,165,824 B each |
| OCI amd64 / arm64 unpacked rootfs | 20,709,376 B / 19,255,296 B | 27,262,976 B each |
| Offline `version` | 6.399 ms median / 7.182 ms p95 | 10 ms / 15 ms |
| Offline maximum RSS | 18.043 MiB p95 | 24 MiB p95 |
| Warm start to readiness | 122.113 ms median / 137.960 ms p95 | 500 ms p95 |
| Ready-idle DANS | 27.375 MiB VmRSS | 64 MiB |
| PostgreSQL authorization, 1 / 100 tuples | 591.976 us / 1.117807 ms | evidence-only |

Representative benchmarks use `testing.B.Loop`, fixed inputs, correctness
checks, and allocation reporting. Five three-second runs are retained as the
descriptive starting point. Shared-runner `ns/op` values are not hard gates;
future performance claims require same-host before/after measurements with
enough repetitions for the selected confidence level. No production metrics or
profiling route and no speculative optimization was added.

Fresh OCI smoke checks passed with the unpacked-rootfs gate and command-surface checks:

- linux/amd64: 20,709,376 bytes;
- linux/arm64: 19,255,296 bytes.
