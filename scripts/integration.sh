#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
compose_file=$root/integration/compose.yaml

if [ "$#" -eq 0 ] && [ -z "${POSTGRES_IMAGE:-}" ]; then
	"$0" postgres:16.14
	"$0" postgres:18.4
	exit 0
fi

postgres_image=${1:-${POSTGRES_IMAGE:-}}
case "$postgres_image" in
	postgres:16.14 | postgres:18.4) ;;
	*)
		printf '%s\n' 'usage: scripts/integration.sh [postgres:16.14|postgres:18.4]' >&2
		exit 2
		;;
esac

for command in curl dig docker jq mktemp; do
	command -v "$command" >/dev/null 2>&1 || {
		printf '%s\n' "integration: missing required command: $command" >&2
		exit 2
	}
done

major_minor=${postgres_image#postgres:}
project=dans-qa-$(printf '%s' "$major_minor" | tr . -)-$$
work=$(mktemp -d "${TMPDIR:-/tmp}/dans-qa.XXXXXX")
log_dir=${DANS_QA_LOG_DIR:-$work/logs}
export POSTGRES_IMAGE=$postgres_image
export DANS_IMAGE=dans-integration:$project
port_base=${DANS_QA_PORT_BASE:-$(( 20000 + ($$ % 20000) ))}
export DANS_A_PORT=${DANS_A_PORT:-$port_base}
export DANS_B_PORT=${DANS_B_PORT:-$(( port_base + 1 ))}
export POWERDNS_PORT=${POWERDNS_PORT:-$(( port_base + 2 ))}
export DANS_BAD_KEY_PORT=${DANS_BAD_KEY_PORT:-$(( port_base + 3 ))}
footprint_container=

compose() {
	docker compose --project-name "$project" --file "$compose_file" "$@"
}

collect_logs() {
	mkdir -p "$log_dir"
	compose ps --all >"$log_dir/compose-ps.txt" 2>&1 || true
	compose logs --no-color --timestamps >"$log_dir/compose.log" 2>&1 || true
	printf '%s\n' "integration: failure logs: $log_dir" >&2
}

cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	if [ "$status" -ne 0 ]; then
		collect_logs
	fi
	if [ -n "$footprint_container" ]; then
		docker container rm "$footprint_container" >/dev/null 2>&1 || true
	fi
	if [ "${DANS_QA_KEEP:-0}" != 1 ]; then
		compose --profile faults --profile tools down --volumes --remove-orphans >/dev/null 2>&1 || true
		docker image rm "$DANS_IMAGE" >/dev/null 2>&1 || true
	fi
	if [ -z "${DANS_QA_LOG_DIR:-}" ]; then
		rm -rf "$work"
	fi
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

fail() {
	printf '%s\n' "integration: $*" >&2
	exit 1
}

wait_for() {
	label=$1
	shift
	deadline=$(( $(date +%s) + 90 ))
	while [ "$(date +%s)" -le "$deadline" ]; do
		if "$@" >/dev/null 2>&1; then
			return 0
		fi
	done
	fail "timed out waiting for $label"
}

http_is() {
	expected=$1
	url=$2
	status=$(curl --silent --show-error --max-time 3 --output /dev/null --write-out '%{http_code}' "$url" 2>/dev/null || true)
	[ "$status" = "$expected" ]
}

assert_http() {
	expected=$1
	url=$2
	shift 2
	status=$(curl --silent --show-error --max-time 5 --output "$work/http-body" --write-out '%{http_code}' "$@" "$url" || true)
	[ "$status" = "$expected" ] || fail "$url returned HTTP $status, want $expected; body: $(tr '\n' ' ' <"$work/http-body")"
}

toxiproxy() {
	compose exec -T toxiproxy /toxiproxy-cli --host http://127.0.0.1:8474 "$@"
}

cli() {
	instance=$1
	token=$2
	shift 2
	compose run --rm --no-deps -T \
		-e DANS_ENDPOINT="http://$instance:8080/api/v1" \
		-e DANS_API_TOKEN="$token" \
		-e DANS_OUTPUT=json \
		cli "$@"
}

cli_data() {
	data=$1
	instance=$2
	token=$3
	shift 3
	printf '%s\n' "$data" | cli "$instance" "$token" "$@" --data=-
}

pdns_cli() {
	compose exec -T \
		-e DANS_ENDPOINT=http://powerdns:8081/api/v1 \
		-e DANS_API_TOKEN=dans_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA \
		-e DANS_OUTPUT=json \
		dans-a /usr/local/bin/dans "$@"
}

pdns_cli_data() {
	data=$1
	shift
	printf '%s\n' "$data" | pdns_cli "$@" --data=-
}

expect_cli_error() {
	label=$1
	expected=$2
	instance=$3
	token=$4
	shift 4
	if cli "$instance" "$token" "$@" >"$work/cli-out" 2>"$work/cli-err"; then
		fail "$label unexpectedly succeeded"
	fi
	grep -Fq "$expected" "$work/cli-err" || fail "$label did not report $expected: $(tr '\n' ' ' <"$work/cli-err")"
}

expect_cli_data_error() {
	label=$1
	expected=$2
	data=$3
	instance=$4
	token=$5
	shift 5
	if cli_data "$data" "$instance" "$token" "$@" >"$work/cli-out" 2>"$work/cli-err"; then
		fail "$label unexpectedly succeeded"
	fi
	grep -Fq "$expected" "$work/cli-err" || fail "$label did not report $expected: $(tr '\n' ' ' <"$work/cli-err")"
}

query_dns() {
	name=$1
	type=$2
	dig @127.0.0.1 -p "$dns_port" +time=2 +tries=1 +noall +answer "$name" "$type"
}

assert_dns_contains() {
	name=$1
	type=$2
	value=$3
	answer=$(query_dns "$name" "$type")
	printf '%s\n' "$answer" | grep -Fq "$value" || fail "$name $type answer did not contain $value: $answer"
}

assert_dns_absent() {
	name=$1
	type=$2
	answer=$(query_dns "$name" "$type")
	[ -z "$answer" ] || fail "$name $type unexpectedly answered: $answer"
}

binding_for() {
	zone_name=$1
	cli dans-a "$operator_token" bindings list --zone-name "$zone_name" \
		| jq -er '.items | map(select(.status == "active")) | first | .id'
}

create_delegation() {
	body=$1
	cli_data "$body" dans-a "$operator_token" delegations create
}

printf '%s\n' "integration: PostgreSQL $major_minor"
compose build dans-a
footprint_container=$(docker container create "$DANS_IMAGE" version)
rootfs_bytes=$(docker container inspect --size --format '{{.SizeRootFs}}' "$footprint_container")
"$root/scripts/footprint.sh" image-rootfs-bytes "$rootfs_bytes" 'OCI root filesystem'
docker container rm "$footprint_container" >/dev/null
footprint_container=
printf '%s\n' "integration: OCI root filesystem $rootfs_bytes bytes"
compose up --detach postgres powerdns toxiproxy
wait_for PostgreSQL compose exec -T postgres pg_isready -h 127.0.0.1 -U dans_ddl -d dans
toxiproxy create --listen 0.0.0.0:18081 --upstream powerdns:8081 powerdns >/dev/null

compose run --rm --no-deps -T \
	-e DANS_DATABASE_URL='postgres://dans_ddl:dans-ddl@postgres/dans?sslmode=disable' \
	-e DANS_OUTPUT=json dans-a db migrate >/dev/null
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U dans_ddl -d dans \
	--set=database_name=dans --set=schema_name=public --set=runtime_role=dans_runtime \
	--file=/dans/runtime.sql >/dev/null
bootstrap=$(compose run --rm --no-deps -T -e DANS_OUTPUT=json dans-a bootstrap \
	--handle operator --display-name 'Integration operator' --token-label integration)
operator_token=$(printf '%s' "$bootstrap" | jq -er '.secret')

compose up --detach dans-a dans-b
a_port=$(compose port dans-a 8080 | awk -F: 'END { print $NF }')
b_port=$(compose port dans-b 8080 | awk -F: 'END { print $NF }')
dns_port=$(compose port --protocol udp powerdns 53 | awk -F: 'END { print $NF }')
a_root=http://127.0.0.1:$a_port
b_root=http://127.0.0.1:$b_port
wait_for 'DANS A readiness' http_is 200 "$a_root/readyz"
wait_for 'DANS B readiness' http_is 200 "$b_root/readyz"

if [ "$(uname -s)" = Linux ]; then
	for service in dans-a dans-b; do
		container=$(compose ps --quiet "$service")
		pid=$(docker inspect --format '{{.State.Pid}}' "$container")
		[ -r "/proc/$pid/status" ] || fail "local Docker PID $pid for $service is not readable in host /proc"
		maximum_rss_kib=0
		for sample in 1 2 3 4 5; do
			rss_kib=$(awk '/^VmRSS:/ { print $2 }' "/proc/$pid/status")
			[ -n "$rss_kib" ] || fail "could not measure $service VmRSS sample $sample"
			"$root/scripts/footprint.sh" rss-kib "$rss_kib" "$service"
			[ "$rss_kib" -le "$maximum_rss_kib" ] || maximum_rss_kib=$rss_kib
		done
		printf '%s\n' "integration: $service ready-idle VmRSS max $maximum_rss_kib KiB across 5 samples"
	done
else
	printf '%s\n' 'integration: ready-idle VmRSS gate runs on native-Linux CI only'
fi

if [ -n "${DANS_QA_PERFORMANCE_DIR:-}" ]; then
	"$root/scripts/measure-runtime.sh" "$compose_file" "$project" dans-a "$a_root" "$DANS_QA_PERFORMANCE_DIR"
fi

forward_zone=$(cli_data '{"name":"example.test.","kind":"Native","nameservers":["ns1.example.test."]}' dans-a "$operator_token" zones create)
forward_zone_id=$(printf '%s' "$forward_zone" | jq -er '.id')
forward_binding=$(binding_for example.test.)
reverse_zone=$(cli_data '{"name":"2.0.192.in-addr.arpa.","kind":"Native","nameservers":["ns1.example.test."]}' dans-b "$operator_token" zones create)
reverse_zone_id=$(printf '%s' "$reverse_zone" | jq -er '.id')
reverse_binding=$(binding_for 2.0.192.in-addr.arpa.)

direct_identity=$(cli_data '{"kind":"user","handle":"direct-user","display_name":"Direct user"}' dans-a "$operator_token" identities create)
direct_id=$(printf '%s' "$direct_identity" | jq -er '.id')
direct_credential=$(cli_data '{"label":"integration"}' dans-b "$operator_token" identities tokens create "$direct_id")
direct_token=$(printf '%s' "$direct_credential" | jq -er '.secret')
group_identity=$(cli_data '{"kind":"service","handle":"group-service"}' dans-b "$operator_token" identities create)
group_identity_id=$(printf '%s' "$group_identity" | jq -er '.id')
group_credential=$(cli_data '{"label":"integration"}' dans-a "$operator_token" identities tokens create "$group_identity_id")
group_token=$(printf '%s' "$group_credential" | jq -er '.secret')
group=$(cli_data '{"handle":"dns-team","display_name":"DNS team"}' dans-a "$operator_token" groups create)
group_id=$(printf '%s' "$group" | jq -er '.id')
cli dans-b "$operator_token" groups members add "$group_id" "$group_identity_id" >/dev/null
cli dans-b "$operator_token" groups members add "$group_id" "$direct_id" >/dev/null

direct_body=$(printf '{"zone_binding_id":"%s","identity_id":"%s","selectors":[{"kind":"glob","value":"*.shared.example.test."}],"record_types":["A"],"change_kinds":["REPLACE","DELETE","EXTEND","PRUNE"]}' "$forward_binding" "$direct_id")
direct_delegation=$(create_delegation "$direct_body")
direct_delegation_id=$(printf '%s' "$direct_delegation" | jq -er '.id')
overlap_body=$(printf '{"zone_binding_id":"%s","group_id":"%s","selectors":[{"kind":"exact","value":"host.shared.example.test."}],"record_types":["A"]}' "$forward_binding" "$group_id")
create_delegation "$overlap_body" >/dev/null
ptr_body=$(printf '{"zone_binding_id":"%s","group_id":"%s","selectors":[{"kind":"exact","value":"10.2.0.192.in-addr.arpa."}],"record_types":["PTR"]}' "$reverse_binding" "$group_id")
create_delegation "$ptr_body" >/dev/null
replace_body='{"rrsets":[{"changetype":"REPLACE","name":"host.shared.example.test.","type":"A","ttl":120,"records":[{"content":"192.0.2.10"}],"comments":[{"account":"integration","content":"delegated forward RRset"}]}]}'
cli_data "$replace_body" dans-a "$direct_token" rrsets apply "$forward_zone_id" >/dev/null
forward_rrset=$(cli dans-b "$direct_token" rrsets get "$forward_zone_id" host.shared.example.test. A)
printf '%s' "$forward_rrset" | jq -e '.ttl == 120 and .comments[0].content == "delegated forward RRset"' >/dev/null || fail 'forward RRset lost TTL or comments'
assert_dns_contains host.shared.example.test. A 192.0.2.10
cli dans-b "$direct_token" rrsets add-value "$forward_zone_id" host.shared.example.test. A 192.0.2.11 --ttl 120 >/dev/null
assert_dns_contains host.shared.example.test. A 192.0.2.11
cli dans-a "$direct_token" rrsets remove-value "$forward_zone_id" host.shared.example.test. A 192.0.2.10 --ttl 120 >/dev/null
assert_dns_absent_value=$(query_dns host.shared.example.test. A)
printf '%s\n' "$assert_dns_absent_value" | grep -Fq 192.0.2.10 && fail 'PRUNE retained the removed value'
cli dans-b "$direct_token" rrsets delete "$forward_zone_id" host.shared.example.test. A --confirm >/dev/null
assert_dns_absent host.shared.example.test. A

ptr_replace='{"rrsets":[{"changetype":"REPLACE","name":"10.2.0.192.in-addr.arpa.","type":"PTR","ttl":300,"records":[{"content":"host.shared.example.test."}]}]}'
cli_data "$ptr_replace" dans-b "$group_token" rrsets apply "$reverse_zone_id" >/dev/null
ptr_answer=$(dig @127.0.0.1 -p "$dns_port" +time=2 +tries=1 +noall +answer -x 192.0.2.10)
printf '%s\n' "$ptr_answer" | grep -Fq host.shared.example.test. || fail "PTR query did not return delegated target: $ptr_answer"

apex_change='{"rrsets":[{"changetype":"REPLACE","name":"shared.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.20"}]}]}'
expect_cli_data_error 'glob apex write' '403 Forbidden' "$apex_change" dans-a "$direct_token" rrsets apply "$forward_zone_id"
wildcard_change='{"rrsets":[{"changetype":"REPLACE","name":"*.shared.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.21"}]}]}'
expect_cli_data_error 'glob literal-wildcard write' '403 Forbidden' "$wildcard_change" dans-b "$direct_token" rrsets apply "$forward_zone_id"
mixed_change='{"rrsets":[{"changetype":"REPLACE","name":"batch.shared.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.30"}]},{"changetype":"REPLACE","name":"forbidden.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.31"}]}]}'
expect_cli_data_error 'mixed RRset batch' '403 Forbidden' "$mixed_change" dans-a "$direct_token" rrsets apply "$forward_zone_id"
assert_dns_absent batch.shared.example.test. A

cli dans-b "$group_token" zones list >/dev/null
expect_cli_error 'non-operator identity administration' '403 Forbidden' dans-a "$direct_token" identities list
expect_cli_error 'non-operator audit visibility' '403 Forbidden' dans-b "$group_token" audit list
assert_http 403 "$a_root/api/v1/servers/localhost/tsigkeys" -H "X-API-Key: $direct_token"
assert_http 403 "$b_root/api/v1/servers/localhost/config" -H "X-API-Key: $group_token"

metadata='{"kind":"ALLOW-AXFR-FROM","metadata":["192.0.2.0/24"]}'
assert_http 200 "$a_root/api/v1/servers/localhost/zones/$forward_zone_id/metadata/ALLOW-AXFR-FROM" \
	-X PUT -H 'Content-Type: application/json' -H "X-API-Key: $operator_token" --data "$metadata"
assert_http 200 "$b_root/api/v1/servers/localhost/zones/$forward_zone_id/metadata/ALLOW-AXFR-FROM" \
	-H "X-API-Key: $operator_token"
jq -e '.kind == "ALLOW-AXFR-FROM" and .metadata == ["192.0.2.0/24"]' "$work/http-body" >/dev/null || fail 'PowerDNS metadata PUT body was not preserved'

unbound_zone=$(pdns_cli_data '{"name":"unbound.test.","kind":"Native","nameservers":["ns1.example.test."]}' zones create)
unbound_zone_id=$(printf '%s' "$unbound_zone" | jq -er '.id')
unbound_bindings=$(cli dans-a "$operator_token" bindings list --zone-name unbound.test.)
printf '%s' "$unbound_bindings" | jq -e '.items | length == 0' >/dev/null || fail 'direct PowerDNS setup unexpectedly created a DANS binding'
cli dans-b "$operator_token" zones delete "$unbound_zone_id" --confirm >/dev/null
if pdns_cli zones get "$unbound_zone_id" >"$work/cli-out" 2>"$work/cli-err"; then
	fail 'existing unbound PowerDNS zone remained after operator deletion'
fi
grep -Fq '404' "$work/cli-err" || fail "deleted unbound PowerDNS zone did not report 404 upstream: $(tr '\n' ' ' <"$work/cli-err")"

cli_data "$replace_body" dans-a "$direct_token" rrsets apply "$forward_zone_id" >/dev/null
successful_audit=$(cli dans-b "$operator_token" audit list --result succeeded)
printf '%s' "$successful_audit" | jq -e '.items | any(.action == "powerdns.zone.patch")' >/dev/null || fail 'successful DNS mutation audit outcome is missing'

missing_change='{"rrsets":[{"changetype":"REPLACE","name":"host.missing.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.40"}]}]}'
expect_cli_data_error 'definite upstream failure' '404' "$missing_change" dans-a "$operator_token" rrsets apply missing.test.
failed_audit=$(cli dans-a "$operator_token" audit list --result failed)
printf '%s' "$failed_audit" | jq -e '.items | any(.action == "powerdns.zone.patch")' >/dev/null || fail 'failed DNS mutation audit outcome is missing'

timeout_change='{"rrsets":[{"changetype":"REPLACE","name":"timeout.shared.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.41"}]}]}'
toxiproxy toxic add --type latency --downstream --toxicName response-timeout --attribute latency=5000 powerdns >/dev/null
expect_cli_data_error 'ambiguous upstream response' '504 Gateway Timeout' "$timeout_change" dans-b "$direct_token" rrsets apply "$forward_zone_id"
toxiproxy toxic remove --toxicName response-timeout powerdns >/dev/null
assert_dns_contains timeout.shared.example.test. A 192.0.2.41
unknown_audit=$(cli dans-a "$operator_token" audit list --result unknown)
printf '%s' "$unknown_audit" | jq -e '.items | any(.action == "powerdns.zone.patch")' >/dev/null || fail 'unknown DNS mutation audit outcome is missing'

audit_failure_change='{"rrsets":[{"changetype":"REPLACE","name":"auditfail.shared.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.42"}]}]}'
toxiproxy toxic add --type latency --downstream --toxicName outcome-delay --attribute latency=1500 powerdns >/dev/null
cli_data "$audit_failure_change" dans-a "$direct_token" rrsets apply "$forward_zone_id" >"$work/audit-failure-out" 2>"$work/audit-failure-err" &
mutation_pid=$!
pending_intent() {
	count=$(compose exec -T postgres psql -X -At -U dans_ddl -d dans -c "SELECT count(*) FROM audit_events AS intent WHERE event_kind = 'dns_intent' AND NOT EXISTS (SELECT 1 FROM audit_events AS outcome WHERE outcome.operation_id = intent.operation_id AND outcome.event_kind = 'dns_outcome')")
	[ "$count" -gt 0 ]
}
wait_for 'persisted mutation intent' pending_intent
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U dans_ddl -d dans -c 'REVOKE INSERT ON TABLE audit_events FROM dans_runtime' >/dev/null
if ! wait "$mutation_pid"; then
	fail "observed PowerDNS response was rewritten after outcome persistence failed: $(tr '\n' ' ' <"$work/audit-failure-err")"
fi
toxiproxy toxic remove --toxicName outcome-delay powerdns >/dev/null
wait_for 'audit failure readiness degradation' http_is 503 "$a_root/readyz"
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U dans_ddl -d dans -c 'GRANT INSERT ON TABLE audit_events TO dans_runtime' >/dev/null
wait_for 'audit persistence recovery' http_is 200 "$a_root/readyz"
assert_dns_contains auditfail.shared.example.test. A 192.0.2.42
pending_audit=$(cli dans-b "$operator_token" audit list --result pending)
printf '%s' "$pending_audit" | jq -e '.items | any(.action == "powerdns.zone.patch")' >/dev/null || fail 'unmatched audit intent was not preserved'

db_failure_change='{"rrsets":[{"changetype":"REPLACE","name":"dbdown.shared.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.43"}]}]}'
compose pause postgres >/dev/null
wait_for 'database outage readiness' http_is 503 "$b_root/readyz"
expect_cli_data_error 'database outage authorization' '503 Service Unavailable' "$db_failure_change" dans-a "$direct_token" rrsets apply "$forward_zone_id"
compose unpause postgres >/dev/null
wait_for 'database recovery' http_is 200 "$a_root/readyz"
assert_dns_absent dbdown.shared.example.test. A

powerdns_failure_change='{"rrsets":[{"changetype":"REPLACE","name":"pdnsdown.shared.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.44"}]}]}'
toxiproxy toggle powerdns >/dev/null
wait_for 'PowerDNS outage readiness' http_is 503 "$a_root/readyz"
expect_cli_data_error 'transient PowerDNS outage after compatibility success' '502 Bad Gateway' "$powerdns_failure_change" dans-b "$direct_token" rrsets apply "$forward_zone_id"
toxiproxy toggle powerdns >/dev/null
wait_for 'PowerDNS recovery' http_is 200 "$b_root/readyz"
assert_dns_absent pdnsdown.shared.example.test. A

compose --profile faults up --detach dans-bad-key
bad_port=$(compose --profile faults port dans-bad-key 8080 | awk -F: 'END { print $NF }')
bad_root=http://127.0.0.1:$bad_port
wait_for 'rejected-key readiness' http_is 503 "$bad_root/readyz"
assert_http 503 "$bad_root/api/v1/servers/localhost/zones" -H "X-API-Key: $operator_token"
compose --profile faults stop dans-bad-key >/dev/null

compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U dans_ddl -d dans -c "INSERT INTO schema_migrations (version, name, checksum) VALUES (99, 'fault', repeat('0', 64))" >/dev/null
wait_for 'schema mismatch readiness' http_is 503 "$a_root/readyz"
assert_http 503 "$a_root/api/v1/servers/localhost/zones" -H "X-API-Key: $operator_token"
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U dans_ddl -d dans -c 'DELETE FROM schema_migrations WHERE version = 99' >/dev/null
wait_for 'schema recovery' http_is 200 "$a_root/readyz"

revoked_body=$(printf '{"zone_binding_id":"%s","identity_id":"%s","selectors":[{"kind":"exact","value":"revoked.example.test."}],"record_types":["A"]}' "$forward_binding" "$direct_id")
revoked_delegation=$(create_delegation "$revoked_body")
revoked_delegation_id=$(printf '%s' "$revoked_delegation" | jq -er '.id')
revoked_change='{"rrsets":[{"changetype":"REPLACE","name":"revoked.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.45"}]}]}'
cli_data "$revoked_change" dans-a "$direct_token" rrsets apply "$forward_zone_id" >/dev/null
cli dans-a "$operator_token" delegations revoke "$revoked_delegation_id" >/dev/null
revoked_second='{"rrsets":[{"changetype":"REPLACE","name":"revoked.example.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.46"}]}]}'
expect_cli_data_error 'cross-instance delegation revocation' '403 Forbidden' "$revoked_second" dans-b "$direct_token" rrsets apply "$forward_zone_id"
assert_dns_contains revoked.example.test. A 192.0.2.45
revoked_answer=$(query_dns revoked.example.test. A)
printf '%s\n' "$revoked_answer" | grep -Fq 192.0.2.46 && fail 'cross-instance revocation allowed a later value'

cli dans-b "$operator_token" delegations revoke "$direct_delegation_id" >/dev/null
overlap_change='{"rrsets":[{"changetype":"REPLACE","name":"host.shared.example.test.","type":"A","ttl":180,"records":[{"content":"192.0.2.47"}]}]}'
cli_data "$overlap_change" dans-a "$direct_token" rrsets apply "$forward_zone_id" >/dev/null
assert_dns_contains host.shared.example.test. A 192.0.2.47

lifecycle_zone=$(cli_data '{"name":"lifecycle.test.","kind":"Native","nameservers":["ns1.example.test."]}' dans-a "$operator_token" zones create)
lifecycle_zone_id=$(printf '%s' "$lifecycle_zone" | jq -er '.id')
lifecycle_binding=$(binding_for lifecycle.test.)
lifecycle_grant_body=$(printf '{"zone_binding_id":"%s","identity_id":"%s","selectors":[{"kind":"exact","value":"host.lifecycle.test."}],"record_types":["A"]}' "$lifecycle_binding" "$direct_id")
lifecycle_grant=$(create_delegation "$lifecycle_grant_body")
lifecycle_grant_id=$(printf '%s' "$lifecycle_grant" | jq -er '.id')
lifecycle_change='{"rrsets":[{"changetype":"REPLACE","name":"host.lifecycle.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.50"}]}]}'
cli_data "$lifecycle_change" dans-b "$direct_token" rrsets apply "$lifecycle_zone_id" >/dev/null
cli dans-a "$operator_token" zones delete "$lifecycle_zone_id" --confirm >/dev/null
retired_binding=$(cli dans-b "$operator_token" bindings get "$lifecycle_binding")
printf '%s' "$retired_binding" | jq -e '.status == "retired"' >/dev/null || fail 'zone deletion did not retire its binding'
retired_grant=$(cli dans-a "$operator_token" delegations get "$lifecycle_grant_id")
printf '%s' "$retired_grant" | jq -e '.revoked_at != null' >/dev/null || fail 'zone deletion did not revoke its grants'
recreated=$(cli_data '{"name":"lifecycle.test.","kind":"Native","nameservers":["ns1.example.test."]}' dans-b "$operator_token" zones create)
recreated_zone_id=$(printf '%s' "$recreated" | jq -er '.id')
recreated_binding=$(binding_for lifecycle.test.)
[ "$recreated_binding" != "$lifecycle_binding" ] || fail 'same-name recreation reused a retired binding'
expect_cli_data_error 'same-name recreation inherited an old grant' '403 Forbidden' "$lifecycle_change" dans-a "$direct_token" rrsets apply "$recreated_zone_id"

reconcile_zone=$(cli_data '{"name":"reconcile.test.","kind":"Native","nameservers":["ns1.example.test."]}' dans-a "$operator_token" zones create)
reconcile_zone_id=$(printf '%s' "$reconcile_zone" | jq -er '.id')
reconcile_binding=$(binding_for reconcile.test.)
reconcile_grant_body=$(printf '{"zone_binding_id":"%s","identity_id":"%s","selectors":[{"kind":"exact","value":"host.reconcile.test."}],"record_types":["A"]}' "$reconcile_binding" "$direct_id")
create_delegation "$reconcile_grant_body" >/dev/null
reconcile_change='{"rrsets":[{"changetype":"REPLACE","name":"host.reconcile.test.","type":"A","ttl":60,"records":[{"content":"192.0.2.51"}]}]}'
cli_data "$reconcile_change" dans-b "$direct_token" rrsets apply "$reconcile_zone_id" >/dev/null
toxiproxy toggle powerdns >/dev/null
expect_cli_error 'unknown zone deletion' '502 Bad Gateway' dans-a "$operator_token" zones delete "$reconcile_zone_id" --confirm
toxiproxy toggle powerdns >/dev/null
wait_for 'PowerDNS recovery after deletion fault' http_is 200 "$a_root/readyz"
delete_intents_before=$(cli dans-a "$operator_token" audit list --action powerdns.zone.delete --result pending --limit 500 | jq -er '.items | length')
expect_cli_error 'retired binding repeat deletion' '409 Conflict' dans-b "$operator_token" zones delete "$reconcile_zone_id" --confirm
delete_intents_after=$(cli dans-b "$operator_token" audit list --action powerdns.zone.delete --result pending --limit 500 | jq -er '.items | length')
[ "$delete_intents_after" = "$delete_intents_before" ] || fail 'retired binding repeat deletion created another DNS intent'
observation=$(cli dans-b "$operator_token" bindings observe "$reconcile_binding")
printf '%s' "$observation" | jq -e '.zone_present == true' >/dev/null || fail 'reconciliation did not observe the retained upstream zone'
rebound_body=$(printf '{"zone_id":"%s"}' "$reconcile_zone_id")
rebound=$(cli_data "$rebound_body" dans-a "$operator_token" bindings rebind "$reconcile_binding")
rebound_id=$(printf '%s' "$rebound" | jq -er '.id')
[ "$rebound_id" != "$reconcile_binding" ] || fail 'rebind reused the retired binding'
expect_cli_data_error 'rebind inherited an old grant' '403 Forbidden' "$reconcile_change" dans-b "$direct_token" rrsets apply "$reconcile_zone_id"

assert_http 200 "$a_root/api/docs" -H "X-API-Key: $operator_token"
jq -e '[.paths | keys[] | select(test("/(test|fault|seed|reset|introspect|metrics|pprof|profil|benchmark)(/|$)"; "i"))] | length == 0' "$work/http-body" >/dev/null || fail 'served production contract exposes a test-only route'
for path in test/seed fault reset introspect metrics pprof profiling benchmark; do
	assert_http 404 "$b_root/api/v1/dans/$path"
done
assert_http 404 "$b_root/metrics"
assert_http 404 "$b_root/debug/pprof/"
compose run --rm --no-deps -T cli --help >"$work/help"
if grep -Eqi '(^|[[:space:]])(seed|reset|fault|introspect|metrics|pprof|profil|benchmark)([[:space:]]|$)' "$work/help"; then
	fail 'production CLI exposes a test-only command'
fi

compose logs --no-color dans-a dans-b >"$work/runtime.log"
for secret in "$operator_token" "$direct_token" "$group_token" dans_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; do
	if grep -Fq "$secret" "$work/runtime.log"; then
		fail 'runtime logs exposed a credential'
	fi
done

printf '%s\n' "integration: PostgreSQL $major_minor passed"
