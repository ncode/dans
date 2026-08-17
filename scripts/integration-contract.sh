#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
compose=$root/integration/compose.yaml
harness=$root/scripts/integration.sh
measurement=$root/scripts/measure-runtime.sh
workflow=$root/.github/workflows/ci.yml

fail() {
	printf '%s\n' "integration contract: $*" >&2
	exit 1
}

[ -f "$compose" ] || fail "missing integration/compose.yaml"
[ -f "$harness" ] || fail "missing scripts/integration.sh"
[ -x "$measurement" ] || fail "missing executable runtime measurement harness"

for service in postgres powerdns toxiproxy dans-a dans-b; do
	grep -Eq "^  $service:" "$compose" || fail "compose service $service is missing"
done

grep -Fq 'postgres:16.14' "$harness" || fail "PostgreSQL 16.14 is not pinned"
grep -Fq 'postgres:18.4' "$harness" || fail "PostgreSQL 18.4 is not pinned"
grep -Fq 'powerdns/pdns-auth-51:5.1.3' "$compose" || fail "PowerDNS 5.1.3 is not pinned"
grep -Fq 'internal: true' "$compose" || fail "the dependency network is not isolated"
grep -Fq 'networks: [control, client, ingress]' "$compose" || fail "DANS lacks a dedicated host-ingress network"
grep -Fq 'ingress:' "$compose" || fail "the host-ingress network is missing"
grep -Fq 'dans-a:' "$compose" || fail "first DANS instance is missing"
grep -Fq 'dans-b:' "$compose" || fail "second DANS instance is missing"
grep -Fq '${DANS_A_PORT:?}' "$compose" || fail "the first DANS loopback port is not explicit"
grep -Fq '${DANS_B_PORT:?}' "$compose" || fail "the second DANS loopback port is not explicit"
grep -Fq '${DANS_BAD_KEY_PORT:?}' "$compose" || fail "the fault-instance loopback port is not explicit"
grep -Fq '${POWERDNS_PORT:?}' "$compose" || fail "the PowerDNS loopback port is not explicit"

if grep -Eq '(^|[^[:alnum:]_])sleep([[:space:]]|$)' "$harness"; then
	fail "fixed sleeps are forbidden; poll a readiness condition"
fi
grep -Fq 'collect_logs' "$harness" || fail "failure logs are not collected"
grep -Fq 'db migrate' "$harness" || fail "migration is not driven by the compiled CLI"
grep -Fq 'bootstrap' "$harness" || fail "bootstrap is not driven by the compiled CLI"
grep -Fq '/readyz' "$harness" || fail "readiness is not polled"
grep -Fq 'dig ' "$harness" || fail "authoritative DNS is not queried"
grep -Fq 'toxiproxy' "$harness" || fail "external upstream fault control is missing"
grep -Fq 'docker compose' "$harness" || fail "the harness does not use the pinned compose environment"
grep -Fq 'pdns_cli_data' "$harness" || fail "an existing unbound PowerDNS zone is not created through the compiled client"
grep -Fq 'unbound.test.' "$harness" || fail "existing unbound PowerDNS zone deletion is not exercised"
grep -Fq "transient PowerDNS outage after compatibility success' '502 Bad Gateway'" "$harness" || fail "transient PowerDNS loss is not distinguished from the compatibility gate"
grep -Fq "retired binding repeat deletion' '409 Conflict'" "$harness" || fail "retired binding history is not enforced before a repeated zone DELETE"
grep -Fq 'docker container inspect --size' "$harness" || fail "the production OCI root filesystem is not measured"
grep -Fq 'footprint.sh" image-rootfs-bytes' "$harness" || fail "the 26 MiB OCI root-filesystem ceiling is not enforced"
grep -Fq '/proc/$pid/status' "$harness" || fail "native-Linux DANS RSS is not measured"
grep -Fq 'footprint.sh" rss-kib' "$harness" || fail "the 64 MiB ready-idle RSS ceiling is not enforced"
grep -Fq 'DANS_QA_PERFORMANCE_DIR' "$harness" || fail "the integration harness cannot emit repeated runtime measurements"
grep -Fq 'for run in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15' "$measurement" || fail "runtime readiness measurement does not repeat 15 starts"
grep -Fq 'for sample in 1 2 3 4 5' "$measurement" || fail "runtime resource measurement does not take five samples"
grep -Fq 'assert_http 404 "$b_root/metrics"' "$harness" || fail "the real-system gate does not reject a production metrics route"
grep -Fq 'assert_http 404 "$b_root/debug/pprof/"' "$harness" || fail "the real-system gate does not reject a production profiling route"

if grep -Eqi '"/api/[^"]*(test|fault|seed|reset|introspect|metrics|pprof|profil|benchmark)' "$root/api/openapi/openapi.json"; then
	fail "production OpenAPI exposes a test-only route"
fi

grep -Fq 'make generate-check' "$workflow" || fail "generation drift is not required in CI"
grep -Fq 'go test ./...' "$workflow" || fail "unit tests are not required in CI"
grep -Fq 'go test -race ./...' "$workflow" || fail "race-tested unit suite is not required in CI"
grep -Fq 'go vet ./...' "$workflow" || fail "static analysis is not required in CI"
grep -Fq 'make benchmark-check' "$workflow" || fail "benchmark correctness smoke is not required in CI"
grep -Fq 'make benchmark-postgres-check' "$workflow" || fail "real PostgreSQL benchmark smoke is not required in CI"
grep -Fq 'scripts/oci-smoke.sh dans-ci-smoke linux/amd64' "$workflow" || fail "CI omits the amd64 OCI footprint gate"
grep -Fq 'scripts/oci-smoke.sh dans-ci-smoke linux/arm64' "$workflow" || fail "CI omits the arm64 OCI footprint gate"
grep -Fq 'DANS_QA_PERFORMANCE_DIR' "$workflow" || fail "CI does not retain repeated runtime measurements"
grep -Eq '^FROM .*golang:\$\{GO_VERSION\}-alpine@sha256:[0-9a-f]{64} AS build$' "$root/Dockerfile" || fail "the Go builder image is not digest-pinned"
grep -Fq 'scripts/integration.sh' "$workflow" || fail "real-system integration is not required in CI"
grep -Fq 'postgres:16.14' "$workflow" || fail "CI omits PostgreSQL 16.14"
grep -Fq 'postgres:18.4' "$workflow" || fail "CI omits PostgreSQL 18.4"
if grep -Fq '\${{' "$workflow"; then
	fail "CI contains escaped GitHub expressions"
fi

printf '%s\n' 'integration contract: ok'
