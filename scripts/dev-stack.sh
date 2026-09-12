#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
compose_file=$root/compose.yaml
credential_dir=$root/.dans/dev
token_file=$credential_dir/operator-token
project_file=$credential_dir/compose-project
ddl_url='postgres://dans_ddl:dans-ddl@postgres/dans?sslmode=disable'
runtime_url='postgres://dans_runtime:dans-runtime@postgres/dans?sslmode=disable'

die() {
	printf '%s\n' "dev stack: $*" >&2
	exit 1
}

if [ -s "$project_file" ]; then
	COMPOSE_PROJECT_NAME=$(sed -n '1p' "$project_file")
elif [ -s "$token_file" ]; then
	COMPOSE_PROJECT_NAME=dans-dev
else
	project_id=$(printf '%s\n' "$root" | cksum | awk '{print $1}')
	COMPOSE_PROJECT_NAME=dans-dev-$project_id
fi
case "$COMPOSE_PROJECT_NAME" in
	''|[!a-z0-9]*|*[!a-z0-9_-]*) die 'invalid local Compose project identity' ;;
esac
export COMPOSE_PROJECT_NAME

compose() {
	docker compose --file "$compose_file" "$@"
}

json_string() {
	field=$1
	value=$(sed -n 's/.*"'"$field"'":"\([^"]*\)".*/\1/p')
	[ -n "$value" ] || die "response omitted $field"
	printf '%s\n' "$value"
}

wait_postgres() {
	attempt=0
	until compose exec -T postgres pg_isready -U dans_ddl -d dans >/dev/null 2>&1; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 60 ] || die 'PostgreSQL did not become ready'
		sleep 1
	done
}

wait_ready() {
	attempt=0
	# `dans health ready` calls the public /readyz endpoint.
	until compose exec -T dans /usr/local/bin/dans health ready >/dev/null 2>&1; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 60 ] || die 'DANS did not become ready'
		sleep 1
	done
}

offline() {
	compose run --rm --no-deps -T \
		-e DANS_DATABASE_URL="$runtime_url" -e DANS_OUTPUT=json dans "$@"
}

ensure_token() {
	[ ! -s "$token_file" ] || {
		chmod 600 "$token_file"
		return
	}
	bootstrap_error=$credential_dir/bootstrap-error
	if credential=$(offline bootstrap --handle dev-operator \
		--display-name 'Dev operator' --token-label local 2>"$bootstrap_error"); then
		rm -f "$bootstrap_error"
	else
		if ! grep -Fq 'database: conflict' "$bootstrap_error"; then
			cat "$bootstrap_error" >&2
			rm -f "$bootstrap_error"
			die 'bootstrap failed'
		fi
		rm -f "$bootstrap_error"
		recovery_label=local-recovery-$(date +%s)-$$
		credential=$(offline recover operator-token --handle dev-operator \
			--token-label "$recovery_label")
	fi
	secret=$(printf '%s\n' "$credential" | json_string secret)
	temporary=$token_file.tmp
	(umask 077 && printf '%s\n' "$secret" >"$temporary")
	chmod 600 "$temporary"
	mv "$temporary" "$token_file"
}

cli_with_token() {
	token=$1
	shift
	compose exec -T \
		-e DANS_ENDPOINT=http://127.0.0.1:8080/api/v1 \
		-e DANS_API_TOKEN="$token" -e DANS_OUTPUT=json \
		dans /usr/local/bin/dans "$@"
}

operator_cli() {
	[ -s "$token_file" ] || die 'operator token is missing; run make up'
	token=$(sed -n '1p' "$token_file")
	cli_with_token "$token" "$@"
}

operator_data() {
	body=$1
	shift
	printf '%s\n' "$body" | operator_cli "$@" --data=-
}

up() {
	mkdir -p "$credential_dir"
	chmod 700 "$credential_dir"
	if [ ! -s "$project_file" ]; then
		(umask 077 && printf '%s\n' "$COMPOSE_PROJECT_NAME" >"$project_file.tmp")
		chmod 600 "$project_file.tmp"
		mv "$project_file.tmp" "$project_file"
	fi
	compose build dans
	compose up --detach postgres powerdns
	wait_postgres
	compose run --rm --no-deps -T -e DANS_DATABASE_URL="$ddl_url" \
		-e DANS_OUTPUT=json dans db migrate >/dev/null
	compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U dans_ddl -d dans \
		--set=database_name=dans --set=schema_name=public \
		--set=runtime_role=dans_runtime --file=/dans/runtime.sql >/dev/null
	ensure_token
	compose up --detach dans
	wait_ready
	printf '%s\n' \
		"DANS: http://127.0.0.1:${DANS_DEV_HTTP_PORT:-8080}" \
		"DNS:  127.0.0.1:${DANS_DEV_DNS_PORT:-1053}" \
		"Console: http://localhost:${DANS_DEV_HTTP_PORT:-8080}/console/" \
		"Console token: $(sed -n '1p' "$token_file")"
}

smoke() {
	compose ps --status running --services | grep -Fxq dans || die 'DANS is not running; run make up'
	wait_ready
	suffix=$(date +%s)-$$
	handle=smoke-$suffix
	zone=smoke-$suffix.test.
	allowed=allowed.$zone
	denied=denied.$zone

	identity=$(operator_data "{\"kind\":\"service\",\"handle\":\"$handle\",\"display_name\":\"Smoke service\"}" identities create)
	identity_id=$(printf '%s\n' "$identity" | json_string id)
	credential=$(operator_data '{"label":"smoke"}' identities tokens create "$identity_id")
	service_token=$(printf '%s\n' "$credential" | json_string secret)
	created_zone=$(operator_data "{\"name\":\"$zone\",\"kind\":\"Native\",\"nameservers\":[\"ns1.$zone\"]}" zones create)
	zone_id=$(printf '%s\n' "$created_zone" | json_string id)
	binding=$(operator_cli bindings list --zone-name "$zone")
	binding_id=$(printf '%s\n' "$binding" | json_string id)
	operator_data "{\"zone_binding_id\":\"$binding_id\",\"identity_id\":\"$identity_id\",\"selectors\":[{\"kind\":\"exact\",\"value\":\"$allowed\"}],\"record_types\":[\"A\"],\"change_kinds\":[\"REPLACE\"]}" delegations create >/dev/null

	cli_with_token "$service_token" rrsets replace "$zone_id" "$allowed" A \
		--ttl 60 --value 192.0.2.10 >/dev/null
	set +e
	denied_output=$(cli_with_token "$service_token" rrsets replace "$zone_id" "$denied" A \
		--ttl 60 --value 192.0.2.11 2>&1)
	denied_status=$?
	set -e
	[ "$denied_status" -eq 1 ] || die "denied RRset write exited $denied_status instead of 1"
	printf '%s\n' "$denied_output" | grep -Fq '403 Forbidden' || die 'denied RRset write did not report 403 Forbidden'

	dns=$(compose run --rm --no-deps -T cli nslookup "$allowed" powerdns 2>&1)
	printf '%s\n' "$dns" | grep -Fq 192.0.2.10 || die 'authoritative DNS answer omitted 192.0.2.10'
	audit=$(operator_cli audit list --actor "$identity_id" --action powerdns.zone.patch \
		--target-type zone_binding --target "$binding_id" --result succeeded)
	printf '%s\n' "$audit" | grep -Fq '"action":"powerdns.zone.patch"' || die 'succeeded RRset audit event is missing'
	printf '%s\n' "$audit" | grep -Fq '"result":"succeeded"' || die 'RRset audit event did not succeed'
	printf '%s\n' "$audit" | grep -Fq '"actor_id":"'"$identity_id"'"' || die 'audit event has the wrong actor'
	printf '%s\n' "$audit" | grep -Fq '"target_id":"'"$binding_id"'"' || die 'audit event has the wrong binding'
	printf '%s\n' "smoke: ok (zone $zone, identity $handle; fixtures retained)"
}

reset() {
	[ "${CONFIRM:-}" = 1 ] || die 'reset deletes local data; rerun with CONFIRM=1'
	rm -f "$token_file" "$token_file.tmp" "$credential_dir/bootstrap-error"
	compose --profile tools down --volumes --remove-orphans
	rm -f "$project_file" "$project_file.tmp"
	rmdir "$credential_dir" 2>/dev/null || true
	rmdir "$root/.dans" 2>/dev/null || true
}

case "${1:-}" in
	up) up ;;
	down) compose --profile tools down --remove-orphans ;;
	status) compose ps; compose exec -T dans /usr/local/bin/dans health ready ;;
	logs) compose logs --follow dans postgres powerdns ;;
	smoke) smoke ;;
	reset) reset ;;
	*) die 'usage: scripts/dev-stack.sh {up|down|status|logs|smoke|reset}' ;;
esac
