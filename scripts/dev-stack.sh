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

json_bool() {
	field=$1
	value=$(sed -n \
		's/.*"'"$field"'":true.*/true/p; s/.*"'"$field"'":false.*/false/p')
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

validate_identity() {
	response=$1
	handle=$(printf '%s\n' "$response" | json_string handle)
	enabled=$(printf '%s\n' "$response" | json_bool enabled)
	operator=$(printf '%s\n' "$response" | json_bool operator)
	[ "$handle" = dev-operator ] || return 1
	[ "$enabled" = true ] || return 1
	[ "$operator" = true ] || return 1
}

validate_credential() {
	token=$1
	if response=$(cli_with_token "$token" me get 2>&1); then
		validate_identity "$response" || {
			printf '%s\n' 'dev stack: credential belongs to an unexpected identity' >&2
			return 1
		}
		return 0
	else
		status=$?
		if [ "$status" -eq 1 ] && printf '%s\n' "$response" | grep -Fq '401 Unauthorized'; then
			return 10
		fi
		printf '%s\n' 'dev stack: credential validation failed' >&2
		return 1
	fi
}

publish_credential() {
	credential=$1
	secret=$(printf '%s\n' "$credential" | json_string secret)
	validate_credential "$secret" || die 'issued credential failed validation'
	temporary=$token_file.tmp
	if ! (umask 077 && printf '%s\n' "$secret" >"$temporary"); then
		rm -f "$temporary"
		die 'write operator token failed'
	fi
	if ! chmod 600 "$temporary"; then
		rm -f "$temporary"
		die 'secure operator token failed'
	fi
	if ! mv "$temporary" "$token_file"; then
		rm -f "$temporary"
		die 'install operator token failed'
	fi
}

ensure_token() {
	if [ -s "$token_file" ]; then
		token=$(sed -n '1p' "$token_file")
		if validate_credential "$token"; then
			chmod 600 "$token_file"
			return
		else
			status=$?
			[ "$status" -eq 10 ] || die 'cached operator credential is unusable'
		fi
	fi
	recovery_label=local-recovery-$(date +%s)-$$
	recovery_error=$credential_dir/recovery-error
	if ! credential=$(offline recover operator-token --handle dev-operator \
		--token-label "$recovery_label" 2>"$recovery_error"); then
		cat "$recovery_error" >&2
		rm -f "$recovery_error"
		die 'operator credential recovery failed'
	fi
	rm -f "$recovery_error"
	publish_credential "$credential"
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

host_prerequisites() {
	for command in curl dig; do
		command -v "$command" >/dev/null 2>&1 || die "make smoke-host requires host command $command"
	done
}

host_published_port() {
	service=$1
	private_port=$2
	protocol=${3:-}
	if [ -n "$protocol" ]; then
		if ! mapping=$(compose port --protocol "$protocol" "$service" "$private_port" 2>/dev/null); then
			die "host port lookup failed for $service $private_port"
		fi
	else
		if ! mapping=$(compose port "$service" "$private_port" 2>/dev/null); then
			die "host port lookup failed for $service $private_port"
		fi
	fi
	[ -n "$mapping" ] ||
		die "host port lookup failed for $service $private_port"
	port=${mapping##*:}
	case "$port" in
		''|*[!0-9]*) die "host port lookup returned an invalid port for $service $private_port" ;;
	esac
	printf '%s\n' "$port"
}

host_http_probe() {
	url=$1
	expected_status=$2
	expected_body=$3
	response=$(mktemp "${TMPDIR:-/tmp}/dans-host-http.XXXXXX") ||
		die 'host HTTP probe could not create a temporary response file'
	if ! status=$(curl --noproxy '*' --connect-timeout 2 --max-time 5 --silent --show-error \
		--output "$response" --write-out '%{http_code}' "$url" 2>/dev/null); then
		rm -f "$response"
		die "host HTTP probe failed (url $url)"
	fi
	if [ "$status" != "$expected_status" ]; then
		rm -f "$response"
		die "host HTTP probe returned status $status (url $url)"
	fi
	case "$expected_body" in
		empty)
		if [ -s "$response" ]; then
			rm -f "$response"
			die "host HTTP probe returned unexpected content (url $url)"
		fi
		;;
	*)
		if ! grep -Fq "$expected_body" "$response"; then
			rm -f "$response"
			die "host HTTP probe returned unexpected content (url $url)"
		fi
		;;
	esac
	rm -f "$response"
}

host_dns_probe() {
	transport=$1
	owner=$2
	value=$3
	dns_port=$(host_published_port powerdns 53 "$transport")
	case "$transport" in
		udp) transport_options='+notcp' ;;
		tcp) transport_options='+tcp' ;;
		*) die "host DNS probe has unknown transport $transport" ;;
	esac
	if ! answer=$(dig @127.0.0.1 -p "$dns_port" +time=2 +tries=1 +norecurse \
		+noedns +ignore +noall +comments +answer "$transport_options" "$owner" A 2>/dev/null); then
		die "host DNS $transport probe failed (port $dns_port)"
	fi
	printf '%s\n' "$answer" | grep -Fq 'status: NOERROR' ||
		die "host DNS $transport probe returned a non-success status (port $dns_port)"
	if printf '%s\n' "$answer" | grep -Eq '^;; flags:.*(^|[[:space:]])tc([;[:space:]]|$)'; then
		die "host DNS $transport probe returned a truncated response (port $dns_port)"
	fi
	printf '%s\n' "$answer" | grep -Eq '^;; flags:.*(^|[[:space:]])aa([;[:space:]]|$)' ||
		die "host DNS $transport probe was not authoritative (port $dns_port)"
	printf '%s\n' "$answer" | awk -v owner="$owner" -v value="$value" \
		'$1 == owner && $4 == "A" && $5 == value { found = 1 } END { exit found ? 0 : 1 }' ||
		die "host DNS $transport probe returned the wrong answer (port $dns_port)"
}

host_smoke() {
	owner=$1
	value=$2
	http_port=$(host_published_port dans 8080)
	http_root=http://127.0.0.1:$http_port
	host_http_probe "$http_root/readyz" 200 empty
	host_http_probe "$http_root/console/" 200 '<div id="root">'
	host_dns_probe udp "$owner" "$value"
	host_dns_probe tcp "$owner" "$value"
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
	bootstrap_error=$credential_dir/bootstrap-error
	if bootstrap_candidate=$(offline bootstrap --handle dev-operator \
		--display-name 'Dev operator' --token-label local 2>"$bootstrap_error"); then
		rm -f "$bootstrap_error"
	else
		if ! grep -Fq 'database: conflict' "$bootstrap_error"; then
			cat "$bootstrap_error" >&2
			rm -f "$bootstrap_error"
			die 'bootstrap failed'
		fi
		rm -f "$bootstrap_error"
		bootstrap_candidate=
	fi
	compose up --detach dans
	wait_ready
	if [ -n "$bootstrap_candidate" ]; then
		publish_credential "$bootstrap_candidate"
	else
		ensure_token
	fi
	printf '%s\n' \
		"DANS: http://127.0.0.1:${DANS_DEV_HTTP_PORT:-8080}" \
		"DNS:  127.0.0.1:${DANS_DEV_DNS_PORT:-1053}" \
		"Console: http://localhost:${DANS_DEV_HTTP_PORT:-8080}/console/" \
		"Console token: $(sed -n '1p' "$token_file")"
}

smoke() {
	host_access=0
	if [ "${1:-}" = host ]; then
		host_access=1
		host_prerequisites
	fi
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
	if [ "$host_access" -eq 1 ]; then
		host_smoke "$allowed" 192.0.2.10
		printf '%s\n' "smoke-host: ok (zone $zone, identity $handle; fixtures retained)"
	else
		printf '%s\n' "smoke: ok (zone $zone, identity $handle; fixtures retained)"
	fi
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
	smoke-host) smoke host ;;
	reset) reset ;;
	*) die 'usage: scripts/dev-stack.sh {up|down|status|logs|smoke|smoke-host|reset}' ;;
esac
