#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
compose=$root/compose.yaml
lifecycle=$root/scripts/dev-stack.sh
makefile=$root/Makefile
readme=$root/README.md

fail() {
	printf '%s\n' "dev contract: $*" >&2
	exit 1
}

[ -f "$compose" ] || fail "missing root compose.yaml"
[ -x "$lifecycle" ] || fail "missing executable scripts/dev-stack.sh"

grep -Fq 'name: ${COMPOSE_PROJECT_NAME:-dans-dev}' "$compose" || fail "Compose project identity is not overridable"
grep -Fq 'image: ${COMPOSE_PROJECT_NAME:-dans-dev}:local' "$compose" || fail "the local image is shared across checkouts"
grep -Fq 'cksum' "$lifecycle" || fail "the stack is not isolated by checkout path"
grep -Fq 'export COMPOSE_PROJECT_NAME' "$lifecycle" || fail "the checkout-scoped project identity is not exported"
grep -Fq 'compose-project' "$lifecycle" || fail "the checkout project identity is not persisted across moves"
if grep -Fq '[ -z "${COMPOSE_PROJECT_NAME:-}" ]' "$lifecycle"; then
	fail "COMPOSE_PROJECT_NAME can detach the database from its checkout credential"
fi

for service in postgres powerdns dans cli; do
	grep -Eq "^  $service:" "$compose" || fail "compose service $service is missing"
done

grep -Fq 'postgres:16.14' "$compose" || fail "PostgreSQL 16.14 is not pinned"
grep -Fq 'powerdns/pdns-auth-51:5.1.3' "$compose" || fail "PowerDNS 5.1.3 is not pinned"
grep -Fq '127.0.0.1:${DANS_DEV_HTTP_PORT:-8080}:8080' "$compose" || fail "DANS is not loopback-bound with an overridable port"
grep -Fq '127.0.0.1:${DANS_DEV_DNS_PORT:-1053}:53/tcp' "$compose" || fail "PowerDNS TCP is not loopback-bound with an overridable port"
grep -Fq '127.0.0.1:${DANS_DEV_DNS_PORT:-1053}:53/udp' "$compose" || fail "PowerDNS UDP is not loopback-bound with an overridable port"
grep -Fq 'DANS_POWERDNS_URL: http://powerdns:8081' "$compose" || fail "DANS does not use the private PowerDNS origin"
grep -Fq 'internal: true' "$compose" || fail "the control network is not isolated"
grep -Eq '^  ingress:' "$compose" || fail "the host-ingress network is missing"
[ "$(grep -Fc 'networks: [control, ingress]' "$compose")" -eq 2 ] || fail "published services lack the host-ingress network"
grep -Fq 'postgres-data:' "$compose" || fail "PostgreSQL data is not persistent"
grep -Fq 'powerdns-data:' "$compose" || fail "PowerDNS data is not persistent"
grep -Fq 'postgres-data:/var/lib/postgresql/data' "$compose" || fail "PostgreSQL data uses the wrong mount target"
grep -Fq 'powerdns-data:/var/lib/powerdns' "$compose" || fail "PowerDNS data uses the wrong mount target"

for target in help up down status logs smoke reset; do
	grep -Eq "^$target:" "$makefile" || fail "make $target is missing"
done

grep -Fq 'db migrate' "$lifecycle" || fail "startup does not run migrations"
grep -Fq 'runtime.sql' "$lifecycle" || fail "startup does not apply runtime grants"
grep -Fq 'bootstrap' "$lifecycle" || fail "startup does not bootstrap a DANS operator"
grep -Fq 'recover operator-token' "$lifecycle" || fail "startup cannot recover a missing local token"
grep -Fq 'database: conflict' "$lifecycle" || fail "startup masks non-conflict bootstrap failures"
grep -Fq 'umask 077' "$lifecycle" || fail "operator token creation lacks a restrictive umask"
grep -Fq 'chmod 600' "$lifecycle" || fail "operator token permissions are not enforced"
grep -Fq '/readyz' "$lifecycle" || fail "startup does not wait for readiness"
grep -Fq 'CONFIRM=1' "$lifecycle" || fail "reset lacks explicit confirmation"
grep -Fq 'delegations create' "$lifecycle" || fail "smoke does not exercise delegated authority"
grep -Fq 'rrsets replace' "$lifecycle" || fail "smoke does not exercise RRset writes"
grep -Fq '403 Forbidden' "$lifecycle" || fail "smoke does not assert the denied status"
grep -Fq 'denied_status' "$lifecycle" || fail "smoke does not assert the denied CLI exit code"
grep -Fq 'audit list --actor' "$lifecycle" || fail "smoke does not scope audit verification to its actor"
grep -Fq -- '--target "$binding_id"' "$lifecycle" || fail "smoke does not scope audit verification to its binding"
grep -Fq '"result":"succeeded"' "$lifecycle" || fail "smoke does not assert the successful audit result"
grep -Eq '(nslookup|dig )' "$lifecycle" || fail "smoke does not query authoritative DNS"
grep -Fq 'down) compose --profile tools down --remove-orphans ;;' "$lifecycle" || fail "make down may remove persistent data"
token_reset_line=$(grep -n 'rm -f "$token_file"' "$lifecycle" | cut -d: -f1)
volume_reset_line=$(grep -n 'down --volumes --remove-orphans' "$lifecycle" | cut -d: -f1)
[ "$token_reset_line" -lt "$volume_reset_line" ] || fail "reset can preserve a stale token after partial volume deletion"

grep -Fq '.dans/' "$root/.gitignore" || fail ".dans credentials are not ignored"
grep -Fxq '.dans' "$root/.dockerignore" || fail ".dans credentials enter the Docker build context"
grep -Fq 'make up' "$readme" || fail "README omits make up"
grep -Fq 'make smoke' "$readme" || fail "README omits make smoke"
grep -Fq 'Run stack lifecycle through Make' "$readme" || fail "README does not identify the credential-aware stack interface"
grep -Fq 'make reset CONFIRM=1' "$readme" || fail "README omits safe reset"
grep -Fq 'DANS_DEV_HTTP_PORT' "$readme" || fail "README omits the HTTP port override"
grep -Fq 'DANS_DEV_DNS_PORT' "$readme" || fail "README omits the DNS port override"
grep -Fq '```mermaid' "$readme" || fail "README omits the system-flow diagram"
grep -Fq 'macOS' "$readme" || fail "README omits macOS support"
grep -Fq 'Linux' "$readme" || fail "README omits Linux support"
if grep -Eqi '(Podman|WSL)' "$readme"; then
	fail "README claims an unsupported workstation path"
fi
if grep -Fq 'openspec/changes/build-dans-v1/' "$readme"; then
	fail "README retains the stale performance link"
fi

sh -n "$lifecycle"
docker compose --file "$compose" config --quiet

printf '%s\n' 'dev contract: ok'
