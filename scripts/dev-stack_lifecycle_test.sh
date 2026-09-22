#!/bin/sh
set -eu

umask 077
root=$(CDPATH= cd -P -- "$(dirname -- "$0")/.." && pwd)
if ! (true <&0) 2>/dev/null; then
	exec 0</dev/null
fi

validate_no_extended_acl() {
	acl_path=$1
	acl_strict=${2:-0}
	acl_listing=$(ls -lde "$acl_path" 2>/dev/null || ls -ld "$acl_path" 2>/dev/null) ||
		fail 'development path ACLs could not be checked'
	acl_mode=$(printf '%s\n' "$acl_listing" | awk 'NR == 1 { print $1 }')
	case "$acl_mode" in
		*+)
			[ "$acl_strict" -eq 0 ] || fail 'development path has an unsupported extended ACL'
			acl_inspected=0
			if command -v getfacl >/dev/null 2>&1; then
				acl_entries=$(getfacl -cpn "$acl_path" 2>/dev/null) ||
					fail 'development path ACL entries could not be inspected'
				current_user_id=$(id -u)
				current_user_name=$(id -un)
				current_group_ids=$(id -G)
				if printf '%s\n' "$acl_entries" | awk -F: \
					-v current_user_id="$current_user_id" \
					-v current_user_name="$current_user_name" '
					/^(default:)?user:[^:]+:/ && $2 != current_user_id && $2 != current_user_name && $2 != 0 && $NF ~ /w/ { found = 1 }
					END { exit found ? 0 : 1 }
				'; then
					fail 'development path has an ACL write grant to another user'
				fi
				if printf '%s\n' "$acl_entries" | awk -F: \
					-v current_group_ids="$current_group_ids" '
					BEGIN { group_count = split(current_group_ids, group_ids, /[[:space:]]+/) }
					/^(default:)?group:[^:]+:/ && $NF ~ /w/ {
						group_is_current = 0
						for (group_index = 1; group_index <= group_count; group_index++) {
							if ($2 == group_ids[group_index]) group_is_current = 1
						}
						if (!group_is_current) found = 1
					}
					END { exit found ? 0 : 1 }
				'; then
					fail 'development path has an ACL write grant to another group'
				fi
				acl_inspected=1
			elif printf '%s\n' "$acl_listing" | awk 'NR > 1 && /allow/ && /(write|delete|add_file|add_subdirectory|append|chown)/ { found = 1 } END { exit found ? 0 : 1 }'; then
				fail 'development path has an ACL write grant'
			fi
			[ "$acl_inspected" -eq 1 ] ||
				[ "$(printf '%s\n' "$acl_listing" | awk 'END { print NR }')" -gt 1 ] ||
				fail 'development path ACL entries could not be inspected'
			;;
	esac
}

validate_checkout_ancestors() {
	ancestor=$root
	while :; do
		[ -d "$ancestor" ] && [ ! -L "$ancestor" ] || fail 'development checkout path is not a trusted directory'
		validate_no_extended_acl "$ancestor"
		ancestor_permissions=$(stat -c '%A' "$ancestor" 2>/dev/null ||
			stat -f '%Sp' "$ancestor" 2>/dev/null) ||
			fail 'development checkout permissions could not be checked'
		ancestor_owner=$(stat -c '%u' "$ancestor" 2>/dev/null ||
			stat -f '%u' "$ancestor" 2>/dev/null) ||
			fail 'development checkout ownership could not be checked'
		current_user_id=$(id -u)
		case "$ancestor_owner" in
			0|"$current_user_id") ;;
			*) fail 'development checkout path is owned by another user' ;;
		esac
		ancestor_writable=$(printf '%s\n' "$ancestor_permissions" |
			awk '{print (substr($0, 6, 1) == "w" || substr($0, 9, 1) == "w") ? 1 : 0}')
		ancestor_sticky=$(printf '%s\n' "$ancestor_permissions" |
			awk '{print (substr($0, 10, 1) == "t" || substr($0, 10, 1) == "T") ? 1 : 0}')
		if [ "$ancestor_writable" -ne 0 ]; then
			[ "$ancestor" != "$root" ] && [ "$ancestor_sticky" -eq 1 ] ||
				fail 'development checkout path is writable by another user'
		fi
		[ "$ancestor" = / ] && break
		ancestor=$(CDPATH= cd -P -- "$ancestor/.." && pwd) ||
			fail 'development checkout parent could not be trusted'
	done
}

token_file=$root/.dans/dev/operator-token
project_file=$root/.dans/dev/compose-project
physical_root=$(CDPATH= cd -P -- "$root" && pwd)
project_id=$(printf '%s\n' "$physical_root" | cksum | awk '{print $1}')
user_id=$(id -u)
project=dans-dev-$user_id-$project_id
lock_dir=/tmp/dans-dev-locks-$user_id
lock_file=$lock_dir/dans-dev-stack-$project_id.lock
project_lock_file=$lock_dir/dans-dev-compose-$project.lock
ownership_marker=$root/.dans/lifecycle-owned
operation_marker=${DANS_DEV_OPERATION_MARKER:-}

fail() {
	printf '%s\n' "dev lifecycle: $*" >&2
	mark_worker_done
	exit 1
}

mark_operation_uncertain() {
	[ -n "$operation_marker" ] || return 0
	(umask 077 && : >"$operation_marker") 2>/dev/null || true
}

mark_worker_done() {
	[ -z "${DANS_DEV_LOCK_WORKER_DONE:-}" ] ||
		(umask 077 && : >"$DANS_DEV_LOCK_WORKER_DONE") 2>/dev/null || true
}

[ -z "${COMPOSE_PROJECT_NAME:-}" ] || fail 'COMPOSE_PROJECT_NAME must not override checkout ownership'
[ ! -e "$root/.dans" ] && [ ! -L "$root/.dans" ] ||
	fail 'checkout already has local development state'

validate_checkout_ancestors

prepare_lock_dir() {
	if [ -L "$lock_dir" ] || { [ -e "$lock_dir" ] && [ ! -d "$lock_dir" ]; }; then
		fail 'development stack lock directory is not a directory'
	fi
	if [ ! -e "$lock_dir" ]; then
		(umask 077 && mkdir "$lock_dir") || [ -d "$lock_dir" ] ||
			fail 'could not create development stack lock directory'
	fi
	[ -d "$lock_dir" ] && [ ! -L "$lock_dir" ] ||
		fail 'development stack lock directory is not a trusted directory'
	[ -O "$lock_dir" ] || fail 'development stack lock directory is not owned by this user'
	validate_no_extended_acl "$lock_dir" 1
	chmod 700 "$lock_dir" 2>/dev/null || fail 'development stack lock directory is not owned by this user'
	[ -r "$lock_dir" ] && [ -w "$lock_dir" ] && [ -x "$lock_dir" ] ||
		fail 'development stack lock directory is not accessible'
}

prepare_lock_path() {
	lock_path=$1
	lock_parent=${lock_path%/*}
	[ "$lock_parent" = "$lock_path" ] && lock_parent=.
	validate_no_extended_acl "$lock_parent" 1
	[ ! -L "$lock_path" ] && { [ ! -e "$lock_path" ] || [ -d "$lock_path" ]; } ||
		fail 'development stack lock path is not a trusted directory'
	[ ! -e "$lock_path" ] || validate_no_extended_acl "$lock_path" 1
}

command -v pgrep >/dev/null 2>&1 || fail 'development stack locking requires pgrep'

process_identity() {
	process_id=$1
	if [ -r "/proc/$process_id/stat" ]; then
		awk '{
			prefix = "^[0-9]+ \\(.*\\) [[:alpha:]] "
			matched = match($0, prefix)
			if (!matched) exit 1
			stat_fields = substr($0, RSTART + RLENGTH)
			split(stat_fields, fields, " ")
			if (fields[19] == "") exit 1
			print fields[19]
		}' "/proc/$process_id/stat" 2>/dev/null
	else
		process_start=$(ps -p "$process_id" -o lstart= 2>/dev/null | tr -d '[:space:]')
		[ -n "$process_start" ] || return 1
		printf '%s\n' "$process_start"
	fi
}

process_running() {
	process_id=$1
	if [ -r "/proc/$process_id/stat" ]; then
		process_state=$(awk '{
			line = $0
			sub(/^[0-9]+ \(.*\) /, "", line)
			print substr(line, 1, 1)
		}' "/proc/$process_id/stat")
		[ "$process_state" != Z ]
	elif process_state=$(ps -p "$process_id" -o state= 2>/dev/null); then
		process_state=$(printf '%s' "$process_state" | tr -d '[:space:]')
		case "$process_state" in
			''|Z*) return 1 ;;
			*) return 0 ;;
				esac
	else
		kill -0 "$process_id" 2>/dev/null
	fi
}

process_parent() {
	process_id=$1
	if [ "$process_id" = "$$" ] && [ -n "${PPID:-}" ]; then
		printf '%s\n' "$PPID"
	elif [ -r "/proc/$process_id/stat" ]; then
		awk '{
			prefix = "^[0-9]+ \\(.*\\) [[:alpha:]] "
			matched = match($0, prefix)
			if (!matched) exit 1
			stat_fields = substr($0, RSTART + RLENGTH)
			split(stat_fields, fields, " ")
			if (fields[1] == "") exit 1
			print fields[1]
		}' "/proc/$process_id/stat"
	else
		parent_id=$(ps -p "$process_id" -o ppid= 2>/dev/null | tr -d '[:space:]')
		case "$parent_id" in
			''|*[!0-9]*) return 1 ;;
			*) printf '%s\n' "$parent_id" ;;
		esac
	fi
}

process_is_descendant() {
	ancestor_id=$1
	candidate_id=${2:-$$}
	ancestor_steps=0
	while [ "$candidate_id" != "$ancestor_id" ]; do
		[ "$ancestor_steps" -lt 64 ] || return 1
		candidate_parent=$(process_parent "$candidate_id" || true)
		case "$candidate_parent" in
			''|0|*[!0-9]*) return 1 ;;
		esac
		[ "$candidate_parent" != "$candidate_id" ] || return 1
		candidate_id=$candidate_parent
		ancestor_steps=$((ancestor_steps + 1))
	done
}

process_group_id() {
	process_id=$1
	if [ -r "/proc/$process_id/stat" ]; then
		awk '{
			prefix = "^[0-9]+ \\(.*\\) [[:alpha:]] "
			matched = match($0, prefix)
			if (!matched) exit 1
			stat_fields = substr($0, RSTART + RLENGTH)
			split(stat_fields, fields, " ")
			if (fields[2] == "") exit 1
			print fields[2]
		}' "/proc/$process_id/stat"
	else
		group_id=$(ps -p "$process_id" -o pgid= 2>/dev/null | tr -d '[:space:]')
		case "$group_id" in
			''|*[!0-9]*) return 1 ;;
			*) printf '%s\n' "$group_id" ;;
		esac
	fi
}

process_group_isolated() {
	candidate_group_id=$1
	caller_group_id=$(process_group_id "$$" || true)
	[ -n "$caller_group_id" ] &&
		[ "$candidate_group_id" != "$caller_group_id" ] &&
		[ "$candidate_group_id" != 0 ]
}

capture_group_baseline() {
	baseline_group_id=$1
	baseline_file=$2
	baseline_status=0
	pgrep -g "$baseline_group_id" >"$baseline_file.members" 2>/dev/null || baseline_status=$?
	[ "$baseline_status" -le 1 ] || {
		rm -f "$baseline_file" "$baseline_file.members"
		return 1
	}
	: >"$baseline_file" || return 1
	while IFS= read -r baseline_member; do
		[ -n "$baseline_member" ] || continue
		baseline_identity=$(process_identity "$baseline_member" || true)
		if [ -n "$baseline_identity" ]; then
			printf '%s/%s\n' "$baseline_member" "$baseline_identity" >>"$baseline_file"
		elif kill -0 "$baseline_member" 2>/dev/null; then
			rm -f "$baseline_file" "$baseline_file.members"
			return 1
		fi
	done <"$baseline_file.members"
	rm -f "$baseline_file.members"
}

group_has_trusted_member() {
	[ -n "${shutdown_group_id:-}" ] || return 1
	for tracked_record in $shutdown_records; do
		tracked_pid=${tracked_record%%/*}
		tracked_identity=${tracked_record#*/}
		[ "$(process_identity "$tracked_pid" || true)" = "$tracked_identity" ] || continue
		[ "$(process_group_id "$tracked_pid" || true)" = "$shutdown_group_id" ] && return 0
	done
	return 1
}

wait_for_lock_owner_record() {
	lock_ready_file=${DANS_DEV_LOCK_OWNER_READY:-}
	[ -n "$lock_ready_file" ] || return 0
	lock_ready_attempt=0
	while [ ! -f "$lock_ready_file" ]; do
		lock_parent=${DANS_DEV_LOCK_OWNER_PARENT:-}
		if [ -n "$lock_parent" ] && ! kill -0 "$lock_parent" 2>/dev/null; then
			fail 'development stack lock owner exited before publishing its record'
		fi
		[ "$lock_ready_attempt" -lt 30 ] || fail 'development stack lock owner record was not published'
		lock_ready_attempt=$((lock_ready_attempt + 1))
		sleep 1
	done
}

validate_lock_owner() {
	owner_lock_path=$1
	[ -d "$owner_lock_path" ] && [ ! -L "$owner_lock_path" ] && [ -O "$owner_lock_path" ] || return 1
	[ -f "$owner_lock_path/pid" ] && [ ! -L "$owner_lock_path/pid" ] || return 1
	owner_pid=$(sed -n '1p' "$owner_lock_path/pid" 2>/dev/null || true)
	owner_identity=$(sed -n '2p' "$owner_lock_path/pid" 2>/dev/null || true)
	case "$owner_pid" in
		''|0|*[!0-9]*) return 1 ;;
	esac
	[ -n "$owner_identity" ] || return 1
	process_running "$owner_pid" || return 1
	current_owner_identity=$(process_identity "$owner_pid" || true)
	[ "$current_owner_identity" = "$owner_identity" ] || return 1
	process_is_descendant "$owner_pid"
}

track_process_tree() {
	track_pid=$1
	track_expected_parent=${2:-}
	if [ -n "$track_expected_parent" ] &&
		[ "$(process_parent "$track_pid" || true)" != "$track_expected_parent" ]; then
		return 0
	fi
	track_identity=$(process_identity "$track_pid" || true)
	[ -n "$track_identity" ] || return 0
	if [ -n "$track_expected_parent" ] &&
		[ "$(process_parent "$track_pid" || true)" != "$track_expected_parent" ]; then
		return 0
	fi
	track_seen=0
	for tracked_record in $shutdown_records; do
		[ "${tracked_record%%/*}" = "$track_pid" ] && track_seen=1
	done
	[ "$track_seen" -eq 1 ] || shutdown_records="$shutdown_records $track_pid/$track_identity"
	track_children=$(pgrep -P "$1" 2>/dev/null || true)
	for descendant_pid in $track_children; do
		track_process_tree "$descendant_pid" "$1"
	done
}

capture_process_tree() {
	captured_process_pid=$1
	captured_process_parent=${2:-}
	shutdown_records=
	track_process_tree "$captured_process_pid" "$captured_process_parent"
	captured_process_records=$shutdown_records
}

captured_process_matches() {
	captured_expected_pid=$1
	captured_expected_identity=$2
	for captured_record in $captured_process_records; do
		[ "${captured_record%%/*}" = "$captured_expected_pid" ] || continue
		[ "${captured_record#*/}" = "$captured_expected_identity" ] && return 0
	done
	return 1
}

has_trusted_record() {
	for tracked_record in $shutdown_records; do
		tracked_pid=${tracked_record%%/*}
		[ "$tracked_pid" = "$shutdown_pid" ] && continue
		tracked_identity=${tracked_record#*/}
		[ "$(process_identity "$tracked_pid" || true)" = "$tracked_identity" ] && return 0
	done
	return 1
}

signal_tracked_processes() {
	track_signal=$1
	for tracked_record in $shutdown_records; do
		tracked_pid=${tracked_record%%/*}
		tracked_identity=${tracked_record#*/}
		[ "$(process_identity "$tracked_pid" || true)" = "$tracked_identity" ] &&
			kill "-$track_signal" "$tracked_pid" 2>/dev/null || true
	done
}

reap_stopped_process() {
	reap_pid=$1
	if ! process_running "$reap_pid"; then
		wait "$reap_pid" 2>/dev/null || true
	fi
}

shutdown_process() {
	shutdown_pid=$1
	shutdown_grace=$2
	shutdown_delegate=$3
	shutdown_expected_group=${4:-}
	shutdown_expected_identity=${5:-}
	shutdown_seed_records=${6:-}
	[ -n "$shutdown_grace" ] || shutdown_grace=10
	shutdown_records=$shutdown_seed_records
	[ -n "$shutdown_expected_identity" ] || return 0
	shutdown_current_identity=$(process_identity "$shutdown_pid" || true)
	shutdown_root_matches=0
	[ "$shutdown_current_identity" = "$shutdown_expected_identity" ] && shutdown_root_matches=1
	shutdown_identity=$shutdown_expected_identity
	[ "$shutdown_root_matches" -eq 1 ] || has_trusted_record || return 0
	if [ "$shutdown_root_matches" -eq 1 ]; then
		shutdown_group_id=${shutdown_expected_group:-$(process_group_id "$shutdown_pid" || true)}
	else
		shutdown_group_id=$shutdown_expected_group
	fi
	shutdown_group=0
	[ -n "$shutdown_group_id" ] || shutdown_group_id=$shutdown_pid
	[ "$shutdown_root_matches" -eq 1 ] && track_process_tree "$shutdown_pid"
	[ -n "$shutdown_expected_group" ] &&
		group_has_trusted_member &&
		process_group_isolated "$shutdown_group_id" &&
		kill -0 "-$shutdown_group_id" 2>/dev/null && shutdown_group=1
	shutdown_attempt=0
	while :; do
		shutdown_root_matches=0
		[ -n "$shutdown_expected_identity" ] &&
			[ "$(process_identity "$shutdown_pid" || true)" = "$shutdown_expected_identity" ] &&
			shutdown_root_matches=1
		if [ "$shutdown_root_matches" -eq 0 ] &&
			[ "$shutdown_group" -eq 1 ] &&
			! group_has_trusted_member; then
			shutdown_group=0
		fi
		if [ "$shutdown_root_matches" -eq 1 ] && kill -0 "$shutdown_pid" 2>/dev/null; then
			track_process_tree "$shutdown_pid"
			if [ "$shutdown_group" -eq 1 ]; then
				if [ "$shutdown_delegate" -eq 1 ]; then
					if [ "$shutdown_root_matches" -eq 1 ]; then
						kill -TERM "$shutdown_pid" 2>/dev/null || true
					else
						signal_tracked_processes TERM
					fi
				else
					if ! kill -TERM "-$shutdown_group_id" 2>/dev/null; then
						signal_tracked_processes TERM
					fi
				fi
				[ "$shutdown_delegate" -eq 1 ] || signal_tracked_processes TERM
			else
				if [ "$shutdown_delegate" -eq 1 ]; then
					if [ "$shutdown_root_matches" -eq 1 ]; then
						kill -TERM "$shutdown_pid" 2>/dev/null || true
					else
						signal_tracked_processes TERM
					fi
				else
					signal_tracked_processes TERM
				fi
			fi
		fi
		[ "$shutdown_root_matches" -eq 1 ] || group_has_trusted_member || shutdown_group=0
		alive=0
		if [ "$shutdown_group" -eq 1 ]; then
			[ "$shutdown_root_matches" -eq 1 ] && process_running "$shutdown_pid" && alive=1
			if [ "$alive" -eq 0 ]; then
				for tracked_record in $shutdown_records; do
					tracked_pid=${tracked_record%%/*}
					[ "$tracked_pid" = "$shutdown_pid" ] && continue
					tracked_identity=${tracked_record#*/}
					[ "$(process_identity "$tracked_pid" || true)" = "$tracked_identity" ] &&
						process_running "$tracked_pid" && alive=1
				done
			fi
		else
			for tracked_record in $shutdown_records; do
				tracked_pid=${tracked_record%%/*}
			tracked_identity=${tracked_record#*/}
				[ "$(process_identity "$tracked_pid" || true)" = "$tracked_identity" ] &&
					process_running "$tracked_pid" && alive=1
			done
		fi
		[ "$alive" -eq 0 ] && break
		[ "$shutdown_attempt" -lt "$shutdown_grace" ] || break
		shutdown_attempt=$((shutdown_attempt + 1))
		sleep 1
	done
	if [ "$shutdown_group" -eq 1 ] &&
		group_has_trusted_member &&
		kill -0 "-$shutdown_group_id" 2>/dev/null; then
		if ! kill -KILL "-$shutdown_group_id" 2>/dev/null; then
			signal_tracked_processes KILL
		fi
	elif [ "$shutdown_group" -eq 0 ]; then
		signal_tracked_processes KILL
	fi
	if [ "$shutdown_group" -eq 0 ]; then
		shutdown_attempt=0
		while :; do
			alive=0
			for tracked_record in $shutdown_records; do
				tracked_pid=${tracked_record%%/*}
				[ "$tracked_pid" = "$shutdown_pid" ] && continue
				tracked_identity=${tracked_record#*/}
				[ "$(process_identity "$tracked_pid" || true)" = "$tracked_identity" ] &&
					process_running "$tracked_pid" && alive=1
			done
			[ "$alive" -eq 0 ] && break
			[ "$shutdown_attempt" -lt 10 ] || break
			shutdown_attempt=$((shutdown_attempt + 1))
			sleep 1
		done
	fi
	signal_tracked_processes KILL
	reap_stopped_process "$shutdown_pid"
}

shutdown_descendants() {
	shutdown_round=0
	while descendants=$(pgrep -P "$1" 2>/dev/null) && [ -n "$descendants" ]; do
		for shutdown_descendant_pid in $descendants; do
			shutdown_descendant_identity=$(process_identity "$shutdown_descendant_pid" || true)
			[ -n "$shutdown_descendant_identity" ] || continue
			capture_process_tree "$shutdown_descendant_pid" "$1"
			captured_process_matches "$shutdown_descendant_pid" "$shutdown_descendant_identity" || continue
			shutdown_process "$shutdown_descendant_pid" 10 0 '' "$shutdown_descendant_identity" "$captured_process_records"
		done
		shutdown_round=$((shutdown_round + 1))
		[ "$shutdown_round" -lt 4 ] || break
	done
}

diagnose_mkdir_lock() {
	lock_path=$1
	[ -f "$lock_path/pid" ] || fail 'development stack is already in use (lock owner is unknown)'
	owner_pid=$(sed -n '1p' "$lock_path/pid" 2>/dev/null || true)
	owner_identity=$(sed -n '2p' "$lock_path/pid" 2>/dev/null || true)
	case "$owner_pid" in
		''|0|*[!0-9]*) fail 'development stack lock owner is unknown or inaccessible; verify no operation is active before removing its lock directory' ;;
	esac
	[ -n "$owner_identity" ] ||
		fail 'development stack lock owner is unknown or inaccessible; verify no operation is active before removing its lock directory'
	current_owner_identity=$(process_identity "$owner_pid" || true)
	[ -n "$current_owner_identity" ] ||
		fail 'development stack lock owner is unknown or inaccessible; verify no operation is active before removing its lock directory'
	if [ "$current_owner_identity" != "$owner_identity" ]; then
		fail 'development stack lock is stale; recorded owner no longer matches the process at that PID'
	fi
	if process_running "$owner_pid"; then
		fail "development stack is already in use by process $owner_pid"
	fi
	fail 'development stack lock is stale; verify no operation is active, then remove its lock directory'
}

acquire_mkdir_lock() {
	lock_path=$1
	env_name=$2
	shift 2
	lock_acquired=0
	worker_pid=
	worker_group_id=
	worker_records=
	initializing=1
	signal_pending=0
	signal_cleanup() {
		status=$1
		retain_lock=$2
		trap '' HUP INT TERM
		if [ -n "${worker_pid:-}" ]; then
			shutdown_process "$worker_pid" 180 1 "$worker_group_id" "${worker_identity:-}" "${worker_records:-}"
			reap_stopped_process "$worker_pid"
		fi
		if [ "$retain_lock" -eq 0 ] && [ "$lock_acquired" -eq 1 ]; then
			rm -f "$lock_path/pid" "$lock_path/ready" "$lock_path/worker-done"
			rmdir "$lock_path" 2>/dev/null || true
		elif [ "$retain_lock" -eq 1 ] && [ "$lock_acquired" -eq 1 ]; then
			printf '%s\n' 'dev lifecycle: interrupted operation left its lock for manual stale-lock recovery' >&2
		fi
		exit "$status"
	}
	signal_handler() {
		if [ "$initializing" -eq 1 ]; then
			signal_pending=1
		else
			signal_cleanup 143 1
		fi
	}
	trap signal_handler HUP INT TERM
	prepare_lock_path "$lock_path"
	if ! (umask 077 && mkdir "$lock_path") 2>/dev/null; then
		diagnose_mkdir_lock "$lock_path"
	fi
	lock_acquired=1
	set -m
	env "$env_name=1" \
		DANS_DEV_OPERATION_MARKER="$lock_path/operation-uncertain" \
		DANS_DEV_LOCK_WORKER_DONE="$lock_path/worker-done" \
		DANS_DEV_LOCK_OWNER_READY="$lock_path/ready" \
		DANS_DEV_LOCK_OWNER_PARENT="$$" \
		DANS_DEV_LOCK_PARENT_GROUP="$(process_group_id "$$" || true)" \
		DANS_DEV_LIFECYCLE_LOCK_OWNER="${DANS_DEV_LIFECYCLE_LOCK_OWNER:-$lock_file}" \
		DANS_DEV_PROJECT_LOCK_OWNER="${DANS_DEV_PROJECT_LOCK_OWNER:-$project_lock_file}" \
		sh -c '
			wrapper_child_pid=
			wrapper_child_identity=
			wrapper_identity_unavailable=0
			wrapper_interrupted=0
			wrapper_process_identity() {
				wrapper_process_id=$1
				if [ -r "/proc/$wrapper_process_id/stat" ]; then
					IFS= read -r wrapper_process_stat <"/proc/$wrapper_process_id/stat" || return 1
					wrapper_process_stat=${wrapper_process_stat##*) }
					set -- $wrapper_process_stat
					[ -n "${20:-}" ] || return 1
					printf "%s\\n" "${20}"
					return 0
				fi
				wrapper_process_start=$(ps -p "$wrapper_process_id" -o lstart= 2>/dev/null | tr -d "[:space:]")
				[ -n "$wrapper_process_start" ] || return 1
				printf "%s\\n" "$wrapper_process_start"
			}
			wrapper_child_matches() {
				[ -n "$wrapper_child_identity" ] &&
					kill -0 "$wrapper_child_pid" 2>/dev/null &&
					[ "$(wrapper_process_identity "$wrapper_child_pid" || true)" = "$wrapper_child_identity" ]
			}
			wrapper_signal() {
				wrapper_interrupted=1
				[ -n "$wrapper_child_pid" ] && wrapper_child_matches &&
					kill -TERM "$wrapper_child_pid" 2>/dev/null || true
			}
			trap wrapper_signal HUP INT TERM
			set +m
			export DANS_DEV_LOCK_PARENT_WRAPPER_PID=${DANS_DEV_LOCK_WRAPPER_PID:-}
			export DANS_DEV_LOCK_WRAPPER_PID=$$
			export DANS_DEV_LOCK_OWNER_PARENT=$$
			wrapper_parent_group=${DANS_DEV_LOCK_PARENT_GROUP:-}
			wrapper_baseline_file=
			wrapper_baseline_probe_failed=0
			wrapper_cleanup_baseline() {
				[ -z "${wrapper_baseline_file:-}" ] ||
					rm -f "$wrapper_baseline_file" "$wrapper_baseline_file.records"
			}
			trap wrapper_cleanup_baseline 0
			if [ -n "$wrapper_parent_group" ]; then
				wrapper_baseline_file=$(mktemp "$DANS_DEV_LOCK_WORKER_DONE.baseline.XXXXXX") || exit 125
				wrapper_baseline_status=0
				pgrep -g "$wrapper_parent_group" >"$wrapper_baseline_file" 2>/dev/null || wrapper_baseline_status=$?
				[ "$wrapper_baseline_status" -le 1 ] || wrapper_baseline_probe_failed=1
				: >"$wrapper_baseline_file.records" || exit 125
				if [ "$wrapper_baseline_probe_failed" -eq 0 ]; then
					while IFS= read -r wrapper_baseline_member; do
						[ -n "$wrapper_baseline_member" ] || continue
						wrapper_baseline_identity=$(wrapper_process_identity "$wrapper_baseline_member" || true)
						if [ -n "$wrapper_baseline_identity" ]; then
							printf "%s/%s\\n" "$wrapper_baseline_member" "$wrapper_baseline_identity" >>"$wrapper_baseline_file.records"
						elif kill -0 "$wrapper_baseline_member" 2>/dev/null; then
							wrapper_baseline_probe_failed=1
						fi
					done <"$wrapper_baseline_file"
				fi
			fi
			[ "$wrapper_interrupted" -eq 0 ] || exit 143
			"$@" &
			wrapper_child_pid=$!
			wrapper_child_identity=$(wrapper_process_identity "$wrapper_child_pid" || true)
			[ -n "$wrapper_child_identity" ] || wrapper_identity_unavailable=1
			if [ "$wrapper_interrupted" -ne 0 ]; then
				[ "$wrapper_identity_unavailable" -eq 0 ] || exit 125
				wrapper_child_matches || exit 143
				kill -TERM "$wrapper_child_pid" 2>/dev/null || true
			fi
			if wait "$wrapper_child_pid"; then
				wrapper_status=0
			else
				wrapper_status=$?
			fi
			[ -n "${DANS_DEV_LOCK_WORKER_DONE:-}" ] || exit 125
			wrapper_group_id=$(ps -p "$$" -o pgid= 2>/dev/null | tr -d "[:space:]")
			wrapper_group_isolated=0
			if [ -n "$wrapper_group_id" ] && [ -n "$wrapper_parent_group" ]; then
				case "$wrapper_group_id" in
					0|*[!0-9]*) ;;
					*)
						case "$wrapper_parent_group" in
							0|*[!0-9]*) ;;
							*)
								if [ "$wrapper_group_id" != "$wrapper_parent_group" ]; then
									wrapper_group_isolated=1
								fi
								;;
						esac
						;;
				 esac
			fi
			wrapper_group_baseline=0
			if [ "$wrapper_group_isolated" -eq 0 ] &&
				[ -n "$wrapper_parent_group" ] &&
				[ "$wrapper_group_id" = "$wrapper_parent_group" ]; then
				wrapper_group_baseline=1
			fi
			wrapper_members_file=
			wrapper_remaining=0
			if [ "$wrapper_group_isolated" -eq 1 ] || [ "$wrapper_group_baseline" -eq 1 ]; then
				wrapper_members_file=$(mktemp "$DANS_DEV_LOCK_WORKER_DONE.members.XXXXXX") || exit 125
				wrapper_attempt=0
				wrapper_probe_failed=0
				if [ "$wrapper_group_baseline" -eq 1 ] &&
					[ "$wrapper_baseline_probe_failed" -ne 0 ]; then
					wrapper_probe_failed=1
				fi
				while :; do
					wrapper_remaining=0
					if [ "$wrapper_probe_failed" -eq 0 ]; then
						wrapper_pgrep_status=0
						: >"$wrapper_members_file" || wrapper_probe_failed=1
						: >"$wrapper_members_file.records" || wrapper_probe_failed=1
						pgrep -g "$wrapper_group_id" >"$wrapper_members_file" 2>/dev/null || wrapper_pgrep_status=$?
						[ "$wrapper_pgrep_status" -le 1 ] || wrapper_probe_failed=1
						while IFS= read -r wrapper_member; do
							[ -n "$wrapper_member" ] || continue
							wrapper_member_identity=$(wrapper_process_identity "$wrapper_member" || true)
							if [ -z "$wrapper_member_identity" ]; then
								kill -0 "$wrapper_member" 2>/dev/null && wrapper_probe_failed=1
								continue
							fi
							wrapper_baseline_match=0
							if [ "$wrapper_group_baseline" -eq 1 ]; then
								while IFS=/ read -r wrapper_baseline_pid wrapper_baseline_identity; do
									if [ "$wrapper_baseline_pid" = "$wrapper_member" ] &&
										[ "$wrapper_baseline_identity" = "$wrapper_member_identity" ]; then
										wrapper_baseline_match=1
										break
									fi
								done <"$wrapper_baseline_file.records"
							fi
							[ "$wrapper_baseline_match" -eq 1 ] ||
								printf "%s/%s\\n" "$wrapper_member" "$wrapper_member_identity" >>"$wrapper_members_file.records"
							if [ "$wrapper_member" != "$$" ]; then
								[ "$wrapper_baseline_match" -eq 1 ] || wrapper_remaining=1
							fi
						done <"$wrapper_members_file"
					else
						wrapper_remaining=1
					fi
					if [ "$wrapper_probe_failed" -ne 0 ]; then
						wrapper_remaining=1
						wrapper_attempt=30
					fi
					[ "$wrapper_remaining" -eq 0 ] || [ "$wrapper_attempt" -ge 30 ] || {
						wrapper_attempt=$((wrapper_attempt + 1))
						sleep 1
						continue
					}
					break
				done
				if [ "$wrapper_remaining" -ne 0 ]; then
					if [ "$wrapper_group_isolated" -eq 1 ]; then
						while IFS=/ read -r wrapper_member wrapper_member_identity; do
							[ "$wrapper_member" = "$$" ] ||
								{ [ -n "$wrapper_member_identity" ] &&
									[ "$(wrapper_process_identity "$wrapper_member" || true)" = "$wrapper_member_identity" ] &&
									kill -KILL "$wrapper_member" 2>/dev/null || true; }
						done <"$wrapper_members_file.records"
					fi
					rm -f "$wrapper_members_file"
					rm -f "$wrapper_members_file.records"
					exit 125
				fi
				rm -f "$wrapper_members_file"
				rm -f "$wrapper_members_file.records"
				[ -z "$wrapper_baseline_file" ] || {
					rm -f "$wrapper_baseline_file" "$wrapper_baseline_file.records"
				}
			else
				wrapper_descendant_probe_status=0
				pgrep -P "$$" >/dev/null 2>&1 || wrapper_descendant_probe_status=$?
				[ "$wrapper_descendant_probe_status" -eq 1 ] || exit 125
			fi
			if [ "$wrapper_interrupted" -eq 0 ]; then
				if [ -n "${DANS_DEV_LOCK_WORKER_DONE:-}" ] &&
					[ ! -e "$DANS_DEV_LOCK_WORKER_DONE" ]; then
					exit 125
				fi
				exit "$wrapper_status"
			fi
			[ "$wrapper_identity_unavailable" -eq 0 ] || exit 125
			wrapper_child_matches || exit 143
			kill -TERM "$wrapper_child_pid" 2>/dev/null || true
			wrapper_attempt=0
			while wrapper_child_matches; do
				if [ "$wrapper_attempt" -ge 180 ]; then
					wrapper_child_matches && kill -KILL "$wrapper_child_pid" 2>/dev/null || true
					exit 125
				fi
				wrapper_attempt=$((wrapper_attempt + 1))
				sleep 1
			done
			exit 143
		' dans-lock-wrapper "$@" &
	worker_pid=$!
	set +m
	worker_group_id=$(process_group_id "$worker_pid" || true)
	worker_identity=$(process_identity "$worker_pid" || true)
	capture_process_tree "$worker_pid"
	worker_records=$captured_process_records
	if [ -n "${DANS_DEV_LOCK_PUBLISH_DELAY:-}" ]; then
		sleep "$DANS_DEV_LOCK_PUBLISH_DELAY"
	fi
	if [ -z "$worker_identity" ] ||
		! (umask 077 && printf '%s\n%s\n' "$worker_pid" "$worker_identity" >"$lock_path/pid" &&
			: >"$lock_path/ready"); then
		initializing=0
		signal_cleanup 1 0
		fail 'could not initialize development stack lock'
	fi
	initializing=0
	capture_process_tree "$worker_pid"
	worker_records=$captured_process_records
	[ "$signal_pending" -eq 0 ] || signal_cleanup 143 1
	if wait "$worker_pid"; then
		rc=0
	else
		rc=$?
	fi
	if [ -e "$lock_path/operation-uncertain" ]; then
		trap ':' HUP INT TERM
		printf '%s\n' 'dev lifecycle: Docker operation completion is uncertain; lock retained for manual stale-lock recovery' >&2
		return 125
	fi
	if [ "$rc" -eq 125 ] || [ "$rc" -ge 128 ]; then
		shutdown_process "$worker_pid" 180 1 "$worker_group_id" "${worker_identity:-}" "${worker_records:-}"
		reap_stopped_process "$worker_pid"
		trap ':' HUP INT TERM
		printf '%s\n' 'dev lifecycle: worker terminated abnormally; lock retained for manual stale-lock recovery' >&2
		return "$rc"
	fi
	trap ':' HUP INT TERM
	rm -f "$lock_path/pid" "$lock_path/ready" "$lock_path/worker-done"
	rmdir "$lock_path" 2>/dev/null || fail 'could not release development stack lock'
	return "$rc"
}

wait_for_lock_owner_record

if [ "${DANS_DEV_LIFECYCLE_LOCK_HELD:-}" = 1 ]; then
	validate_lock_owner "$lock_file.lockdir" "${DANS_DEV_LIFECYCLE_LOCK_OWNER_IDENTITY:-}" ||
		fail 'development stack lock ownership could not be verified'
else
	prepare_lock_dir
	if acquire_mkdir_lock "$lock_file.lockdir" DANS_DEV_LIFECYCLE_LOCK_HELD "$0" "$@"; then
		acquire_status=0
	else
		acquire_status=$?
	fi
	mark_worker_done
	exit "$acquire_status"
fi

if [ "${DANS_DEV_PROJECT_LOCK_HELD:-}" = 1 ]; then
	validate_lock_owner "$project_lock_file.lockdir" "${DANS_DEV_PROJECT_LOCK_OWNER_IDENTITY:-}" ||
		fail 'development project lock ownership could not be verified'
else
	prepare_lock_dir
	if acquire_mkdir_lock "$project_lock_file.lockdir" DANS_DEV_PROJECT_LOCK_HELD \
		"$0" "$@"; then
		acquire_status=0
	else
		acquire_status=$?
	fi
	mark_worker_done
	exit "$acquire_status"
fi

run=
initialization_cleanup() {
	status=$?
	trap - EXIT
	[ -z "${run:-}" ] || rm -rf "$run"
	mark_worker_done
	exit "$status"
}
trap initialization_cleanup EXIT

run=$(mktemp -d "${TMPDIR:-/tmp}/dans-dev-lifecycle.XXXXXX")
chmod 700 "$run"
run_operation_marker=$run/docker-operation-uncertain
operation_marker=$run_operation_marker
owned=0
run_make_count=0

cleanup_probe() {
	probe_name=$1
	shift
	probe_status_file=$run/$probe_name.status
	probe_start_file=$run/$probe_name.start
	probe_go_file=$run/$probe_name.go
	probe_cancel_file=$run/$probe_name.cancel
	rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
	probe_baseline_file=
	probe_parent_group=$(process_group_id "$$" || true)
	if [ -n "$probe_parent_group" ]; then
		probe_baseline_file=$(mktemp "$run/$probe_name-group-baseline.XXXXXX" || true)
		if [ -n "$probe_baseline_file" ] && ! capture_group_baseline "$probe_parent_group" "$probe_baseline_file"; then
			probe_baseline_file=
		fi
	fi
	set -m
	(
		(umask 077 && : >"$probe_start_file") || exit 125
		while [ ! -e "$probe_go_file" ]; do
			[ ! -e "$probe_cancel_file" ] || exit 125
			sleep 1
		done
		if "$@"; then
			printf '%s\n' 0 >"$probe_status_file"
		else
			probe_command_status=$?
			printf '%s\n' "$probe_command_status" >"$probe_status_file"
		fi
	) >"$run/$probe_name.out" 2>"$run/$probe_name.err" &
	probe_pid=$!
	probe_attempt=0
	while [ ! -e "$probe_start_file" ]; do
		if ! kill -0 "$probe_pid" 2>/dev/null; then
			wait "$probe_pid" 2>/dev/null || true
			rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
			return 1
		fi
		if [ "$probe_attempt" -ge 300 ]; then
			(umask 077 && : >"$probe_cancel_file") 2>/dev/null || true
			kill -TERM "$probe_pid" 2>/dev/null || true
			wait "$probe_pid" 2>/dev/null || true
			rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
			return 1
		fi
		probe_attempt=$((probe_attempt + 1))
		sleep 0.1
	done
	probe_group_id=$(process_group_id "$probe_pid" || true)
	probe_identity=$(process_identity "$probe_pid" || true)
	[ -n "$probe_group_id" ] && [ -n "$probe_identity" ] || {
		(umask 077 && : >"$probe_cancel_file") 2>/dev/null || true
		kill -TERM "$probe_pid" 2>/dev/null || true
		wait "$probe_pid" 2>/dev/null || true
		rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
		return 1
	}
	capture_process_tree "$probe_pid"
	probe_records=$captured_process_records
	set +m
	(umask 077 && : >"$probe_go_file") || {
		kill -TERM "$probe_pid" 2>/dev/null || true
		wait "$probe_pid" 2>/dev/null || true
		rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
		return 1
	}
	probe_attempt=0
	while [ ! -s "$probe_status_file" ]; do
		if ! kill -0 "$probe_pid" 2>/dev/null; then
			wait "$probe_pid" 2>/dev/null || true
			rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
			return 1
		fi
		if [ "$probe_attempt" -ge 30 ]; then
			shutdown_process "$probe_pid" 10 0 '' "$probe_identity" "$probe_records"
			reap_stopped_process "$probe_pid"
			rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
			return 1
		fi
		probe_attempt=$((probe_attempt + 1))
		sleep 1
	done
	probe_status=$(sed -n '1p' "$probe_status_file")
	wait "$probe_pid" 2>/dev/null || true
	probe_saved_baseline_file=$running_group_baseline_file
	running_group_baseline_file=
	if ! process_group_isolated "$probe_group_id"; then
		running_group_baseline_file=$probe_baseline_file
	fi
	probe_drain_status=0
	if ! drain_tracked_process "$probe_pid" "$probe_group_id" "$probe_identity" "$probe_records"; then
		probe_drain_status=1
	fi
	running_group_baseline_file=$probe_saved_baseline_file
	[ -z "$probe_baseline_file" ] || rm -f "$probe_baseline_file" "$probe_baseline_file.members"
	if [ "$probe_drain_status" -ne 0 ]; then
		rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
		return 1
	fi
	rm -f "$probe_status_file" "$probe_start_file" "$probe_go_file" "$probe_cancel_file"
	[ "$probe_status" -eq 0 ]
}

cleanup() {
	status=$?
	cleanup_status=0
	trap - EXIT
	trap ':' HUP INT TERM
	[ -e "$run_operation_marker" ] && operation_ambiguous=1
	if [ "$owned" -eq 1 ] && [ "$operation_ambiguous" -eq 0 ]; then
		cleanup_baseline_file=
		cleanup_parent_group=$(process_group_id "$$" || true)
		if [ -n "$cleanup_parent_group" ]; then
			cleanup_baseline_file=$(mktemp "$run/cleanup-group-baseline.XXXXXX" || true)
			if [ -n "$cleanup_baseline_file" ]; then
				cleanup_baseline_status=0
				pgrep -g "$cleanup_parent_group" >"$cleanup_baseline_file.members" 2>/dev/null ||
					cleanup_baseline_status=$?
				if [ "$cleanup_baseline_status" -gt 1 ]; then
					rm -f "$cleanup_baseline_file" "$cleanup_baseline_file.members"
					cleanup_baseline_file=
				else
					: >"$cleanup_baseline_file" || cleanup_baseline_file=
					if [ -n "$cleanup_baseline_file" ]; then
						while IFS= read -r cleanup_baseline_member; do
							[ -n "$cleanup_baseline_member" ] || continue
							cleanup_baseline_identity=$(process_identity "$cleanup_baseline_member" || true)
							if [ -n "$cleanup_baseline_identity" ]; then
								printf '%s/%s\n' "$cleanup_baseline_member" "$cleanup_baseline_identity" >>"$cleanup_baseline_file"
							elif kill -0 "$cleanup_baseline_member" 2>/dev/null; then
								rm -f "$cleanup_baseline_file" "$cleanup_baseline_file.members"
								cleanup_baseline_file=
								break
							fi
						done <"$cleanup_baseline_file.members"
					fi
				[ -z "$cleanup_baseline_file" ] || rm -f "$cleanup_baseline_file.members"
				fi
			fi
		fi
		cleanup_status_file=$run/cleanup.status
		set -m
		(
			if (cd "$root" && env -u COMPOSE_PROJECT_NAME DANS_DEV_STACK_LOCK_OWNER="$lock_file" \
				DANS_DEV_PROJECT_LOCK_HELD=1 DANS_DEV_PROJECT_LOCK_OWNER="$project_lock_file" \
				make reset CONFIRM=1); then
				printf '%s\n' 0 >"$cleanup_status_file"
			else
				cleanup_command_status=$?
				printf '%s\n' "$cleanup_command_status" >"$cleanup_status_file"
			fi
		) >"$run/cleanup.out" 2>"$run/cleanup.err" &
		cleanup_pid=$!
		cleanup_group_id=$(process_group_id "$cleanup_pid" || true)
		cleanup_identity=$(process_identity "$cleanup_pid" || true)
		capture_process_tree "$cleanup_pid"
		cleanup_records=$captured_process_records
		set +m
		cleanup_attempt=0
		while [ ! -s "$cleanup_status_file" ]; do
			if ! kill -0 "$cleanup_pid" 2>/dev/null; then
				cleanup_status=1
				break
			fi
			if [ "$cleanup_attempt" -ge 60 ]; then
				shutdown_process "$cleanup_pid" 10 0 '' "$cleanup_identity" "$cleanup_records"
				cleanup_status=1
				break
			fi
			cleanup_attempt=$((cleanup_attempt + 1))
			sleep 1
		done
		if [ "$cleanup_status" -eq 0 ]; then
			if [ -s "$cleanup_status_file" ]; then
				cleanup_status=$(sed -n '1p' "$cleanup_status_file")
			fi
		fi
		cleanup_exit_attempt=0
		while process_running "$cleanup_pid"; do
			[ "$cleanup_exit_attempt" -lt 30 ] || break
			cleanup_exit_attempt=$((cleanup_exit_attempt + 1))
			sleep 0.1
		done
		if process_running "$cleanup_pid"; then
			cleanup_status=1
			operation_ambiguous=1
			printf '%s\n' 'dev lifecycle: cleanup worker did not stop within the bounded wait' >&2
		else
			wait "$cleanup_pid" 2>/dev/null || true
			reap_stopped_process "$cleanup_pid"
		fi
		cleanup_saved_baseline_file=$running_group_baseline_file
		running_group_baseline_file=
		if ! process_group_isolated "$cleanup_group_id"; then
			running_group_baseline_file=$cleanup_baseline_file
		fi
		if [ "$cleanup_status" -eq 0 ] &&
			! drain_tracked_process "$cleanup_pid" "$cleanup_group_id" "$cleanup_identity" "$cleanup_records"; then
			cleanup_status=1
		fi
		running_group_baseline_file=$cleanup_saved_baseline_file
		[ -z "$cleanup_baseline_file" ] || rm -f "$cleanup_baseline_file" "$cleanup_baseline_file.members"
		if [ "$cleanup_status" -eq 0 ] &&
			{ [ -e "$root/.dans/dev" ] || [ -e "$project_file" ]; }; then
			cleanup_status=1
		fi
		if [ "$cleanup_status" -eq 0 ]; then
			if ! cleanup_probe cleanup-containers compose ps -aq; then
				cleanup_status=1
			elif [ -n "$(cat "$run/cleanup-containers.out")" ]; then
				cleanup_status=1
			fi
		fi
		if [ "$cleanup_status" -eq 0 ]; then
			if ! cleanup_probe cleanup-volumes docker volume ls -q \
				--filter "label=com.docker.compose.project=$project"; then
				cleanup_status=1
			elif [ -n "$(cat "$run/cleanup-volumes.out")" ]; then
				cleanup_status=1
			fi
		fi
		if [ "$cleanup_status" -eq 0 ]; then
			rm -f "$ownership_marker"
			rmdir "$root/.dans" 2>/dev/null || cleanup_status=$?
		fi
		if [ "$cleanup_status" -ne 0 ]; then
			operation_ambiguous=1
			printf '%s\n' "dev lifecycle: cleanup failed (status $cleanup_status); owned resources may remain" >&2
			sanitize_failure cleanup >&2
		fi
	elif [ "$owned" -eq 1 ]; then
		cleanup_status=1
		printf '%s\n' 'dev lifecycle: cleanup skipped after an interrupted Docker operation; inspect the retained state and locks before recovery' >&2
	fi
	[ -e "$run_operation_marker" ] && operation_ambiguous=1
	rm -rf "$run"
	[ "$operation_ambiguous" -eq 0 ] || [ "$status" -ge 128 ] || status=125
	[ "$status" -ne 0 ] || [ "$cleanup_status" -eq 0 ] || status=1
	mark_worker_done
	exit "$status"
}
running_pid=
running_group_id=
running_identity=
running_records=
running_launch_marker=
running_launch_wait=
running_launch_lease=
running_launch_capture_stderr=1
running_group_baseline_file=
launching=0
pending_signal=0
operation_ambiguous=0
lifecycle_signal() {
	if [ "$launching" -eq 1 ]; then
		pending_signal=1
		return
	fi
	trap '' HUP INT TERM
	mark_operation_uncertain
	if [ -n "${running_pid:-}" ]; then
		operation_ambiguous=1
		shutdown_process "$running_pid" 10 0 "${running_group_id:-}" "${running_identity:-}" "${running_records:-}"
	fi
	clear_stopped_supervised_launch
	exit 143
}
trap cleanup EXIT
trap lifecycle_signal HUP INT TERM

running_processes_drained() {
	for tracked_record in $running_records; do
		tracked_pid=${tracked_record%%/*}
		[ "$tracked_pid" = "$running_pid" ] && continue
		if process_running "$tracked_pid"; then
			tracked_current_identity=$(process_identity "$tracked_pid" || true)
			if [ -z "$tracked_current_identity" ]; then
				return 1
			elif [ "$tracked_current_identity" = "${tracked_record#*/}" ]; then
				return 1
			fi
		fi
	done
	return 0
}

running_group_drained() {
	if [ -z "${running_group_id:-}" ]; then
		return 1
	fi
	if ! process_group_isolated "$running_group_id"; then
		[ -n "${running_group_baseline_file:-}" ] || return 1
		running_group_file=$(mktemp "$lock_dir/process-group.XXXXXX") || return 1
		running_group_status=0
		pgrep -g "$running_group_id" >"$running_group_file" 2>/dev/null || running_group_status=$?
		if [ "$running_group_status" -gt 1 ]; then
			rm -f "$running_group_file"
			return 1
		fi
		running_group_operation_seen=0
		while IFS= read -r running_group_member; do
			[ -n "$running_group_member" ] || continue
			[ "$running_group_member" = "$running_pid" ] && continue
			running_group_identity=$(process_identity "$running_group_member" || true)
			[ -n "$running_group_identity" ] || {
				rm -f "$running_group_file"
				return 1
			}
			running_group_baseline_match=0
			while IFS=/ read -r running_baseline_pid running_baseline_identity; do
				if [ "$running_baseline_pid" = "$running_group_member" ] &&
					[ "$running_baseline_identity" = "$running_group_identity" ]; then
					running_group_baseline_match=1
					break
				fi
			done <"$running_group_baseline_file"
			if [ "$running_group_baseline_match" -eq 0 ]; then
				running_group_operation_member=0
				if process_is_descendant "$running_pid" "$running_group_member"; then
					running_group_operation_member=1
				else
					for tracked_record in $running_records; do
						[ "${tracked_record%%/*}" = "$running_group_member" ] || continue
						[ "${tracked_record#*/}" = "$running_group_identity" ] || continue
						running_group_operation_member=1
						break
					done
				fi
				if [ "$running_group_operation_member" -ne 1 ]; then
					rm -f "$running_group_file"
					return 1
				fi
				running_group_operation_seen=1
			fi
			done <"$running_group_file"
			rm -f "$running_group_file"
		[ "${running_group_operation_seen:-0}" -eq 0 ]
		return $?
	fi
	running_group_file=$(mktemp "$lock_dir/process-group.XXXXXX") || return 1
	running_group_status=0
	pgrep -g "$running_group_id" >"$running_group_file" 2>/dev/null || running_group_status=$?
	if [ "$running_group_status" -gt 1 ]; then
		rm -f "$running_group_file"
		return 1
	fi
	while IFS= read -r running_group_member; do
		[ -n "$running_group_member" ] || continue
		[ "$running_group_member" = "$running_pid" ] && continue
		rm -f "$running_group_file"
		return 1
	done <"$running_group_file"
	rm -f "$running_group_file"
}

terminate_running_group() {
	[ -n "${running_group_id:-}" ] || return 1
	process_group_isolated "$running_group_id" || return 1
	running_group_file=$(mktemp "$lock_dir/process-group.XXXXXX") || return 1
	running_group_status=0
	pgrep -g "$running_group_id" >"$running_group_file" 2>/dev/null || running_group_status=$?
	if [ "$running_group_status" -gt 1 ]; then
		rm -f "$running_group_file"
		return 1
	fi
	running_group_trusted=0
	while IFS= read -r running_group_member; do
		[ -n "$running_group_member" ] || continue
		running_group_identity=$(process_identity "$running_group_member" || true)
		[ -n "$running_group_identity" ] || continue
		for tracked_record in $running_records; do
			tracked_pid=${tracked_record%%/*}
			tracked_identity=${tracked_record#*/}
			[ "$tracked_pid" = "$running_group_member" ] || continue
			[ "$tracked_identity" = "$running_group_identity" ] || continue
			[ "$(process_group_id "$running_group_member" || true)" = "$running_group_id" ] || continue
			running_group_trusted=1
			break
		done
		[ "$running_group_trusted" -eq 1 ] && break
	done <"$running_group_file"
	[ "$running_group_trusted" -eq 1 ] || {
		rm -f "$running_group_file"
		return 1
	}
	while IFS= read -r running_group_member; do
		[ -n "$running_group_member" ] || continue
		[ "$running_group_member" = "$$" ] && continue
		[ "$running_group_member" = "$running_pid" ] && continue
		running_group_identity=$(process_identity "$running_group_member" || true)
		[ -n "$running_group_identity" ] || {
			rm -f "$running_group_file"
			return 1
		}
		[ "$(process_group_id "$running_group_member" || true)" = "$running_group_id" ] || continue
		[ "$(process_identity "$running_group_member" || true)" = "$running_group_identity" ] || continue
		kill -KILL "$running_group_member" 2>/dev/null || true
	done <"$running_group_file"
	rm -f "$running_group_file"
}

drain_running_processes() {
	if running_group_drained && running_processes_drained; then
		return 0
	fi
	terminate_running_group || return 1
	return 1
}

drain_tracked_process() {
	drain_saved_pid=${running_pid:-}
	drain_saved_group_id=${running_group_id:-}
	drain_saved_identity=${running_identity:-}
	drain_saved_records=${running_records:-}
	running_pid=$1
	running_group_id=$2
	running_identity=$3
	running_records=$4
	drain_status=0
	if ! drain_running_processes; then
		drain_status=1
	fi
	running_pid=$drain_saved_pid
	running_group_id=$drain_saved_group_id
	running_identity=$drain_saved_identity
	running_records=$drain_saved_records
	return "$drain_status"
}

clear_supervised_launch() {
	[ -z "${running_launch_marker:-}" ] ||
		rm -f "$running_launch_marker" "$running_launch_marker.ready" \
		"$running_launch_marker.go" "$running_launch_marker.cancel" \
		"$running_launch_marker.status" "$running_launch_marker.finished" \
		"$running_launch_marker.escaped" "$running_launch_marker.lease-ready" \
		"$running_launch_marker.stderr" "$running_launch_marker.stderr.pipe" \
		"$running_launch_marker.watchdog"
	[ -z "${running_launch_wait:-}" ] || rm -f "$running_launch_wait" "$running_launch_wait.writer"
	[ -z "${running_launch_lease:-}" ] || rm -f "$running_launch_lease"
	[ -z "${running_group_baseline_file:-}" ] ||
		rm -f "$running_group_baseline_file" "$running_group_baseline_file.members"
	running_launch_marker=
	running_launch_wait=
	running_launch_lease=
	running_launch_capture_stderr=1
	running_group_baseline_file=
}

clear_stopped_supervised_launch() {
	if [ -n "${running_pid:-}" ] && process_running "$running_pid"; then
		return 0
	fi
	clear_supervised_launch
}

write_supervised_signal() {
	supervised_signal=$1
	[ -n "${running_launch_wait:-}" ] || return 1
	supervised_writer_status_file=$running_launch_wait.writer
	rm -f "$supervised_writer_status_file"
	(
		if printf '%s\n' "$supervised_signal" >"$running_launch_wait"; then
			printf '%s\n' 0 >"$supervised_writer_status_file"
		else
			printf '%s\n' 1 >"$supervised_writer_status_file"
		fi
	) &
	supervised_writer_pid=$!
	supervised_writer_attempt=0
	while [ ! -s "$supervised_writer_status_file" ]; do
		if [ "$supervised_writer_attempt" -ge 30 ]; then
			kill -TERM "$supervised_writer_pid" 2>/dev/null || true
			wait "$supervised_writer_pid" 2>/dev/null || true
			rm -f "$supervised_writer_status_file"
			return 1
		fi
		supervised_writer_attempt=$((supervised_writer_attempt + 1))
		sleep 0.1
	done
	wait "$supervised_writer_pid" 2>/dev/null || true
	supervised_writer_status=$(sed -n '1p' "$supervised_writer_status_file" 2>/dev/null || true)
	rm -f "$supervised_writer_status_file"
	[ "$supervised_writer_status" = 0 ]
}

cancel_supervised_launch() {
	[ -z "${running_launch_marker:-}" ] ||
		(umask 077 && : >"$running_launch_marker.cancel") 2>/dev/null || true
	if [ -n "${running_pid:-}" ]; then
		cancel_attempt=0
		supervisor_signal_sent=0
		while process_running "$running_pid"; do
			[ -z "${running_identity:-}" ] ||
				[ "$(process_identity "$running_pid" || true)" = "$running_identity" ] || break
			kill -CONT "$running_pid" 2>/dev/null || true
			if [ "$supervisor_signal_sent" -eq 0 ] && [ -e "${running_launch_marker:-}.finished" ]; then
				write_supervised_signal cancel || true
				supervisor_signal_sent=1
			fi
			if [ "$cancel_attempt" -ge 30 ]; then
				kill -TERM "$running_pid" 2>/dev/null || true
				sleep 1
				[ "$(process_identity "$running_pid" || true)" = "$running_identity" ] &&
					kill -KILL "$running_pid" 2>/dev/null || true
				break
			fi
			cancel_attempt=$((cancel_attempt + 1))
			sleep 0.1
		done
		wait "$running_pid" 2>/dev/null || true
	fi
	clear_supervised_launch
	running_pid=
	running_group_id=
	running_identity=
	running_records=
	running_launch_wait=
}

launch_supervised() {
	# Detached/daemonizing launchers are unsupported; keep the foreground allowlist explicit.
	case "${1:-}:${2:-}" in
		docker:compose|docker:volume|sh:-c) ;;
		*) return 1 ;;
	esac
	running_launch_capture_stderr=1
	case " $* " in
		*" logs "*) running_launch_capture_stderr=0 ;;
	esac
	running_launch_marker=$(mktemp "$lock_dir/operation-launch.XXXXXX") || return 1
	rm -f "$running_launch_marker"
	running_launch_wait=$running_launch_marker.wait
	running_launch_lease=$running_launch_marker.lease
	mkfifo "$running_launch_wait" || {
		running_launch_wait=
		clear_supervised_launch
		return 1
	}
	mkfifo "$running_launch_lease" || {
		clear_supervised_launch
		return 1
	}
	mkfifo "$running_launch_marker.stderr.pipe" || {
		clear_supervised_launch
		return 1
	}
	exec 9<&0
	sh -c '
		launch_marker=$1
		stderr_capture=$2
		shift 2
		stderr_forward_pid=
		supervisor_watchdog_pid=
		watchdog_process_identity() {
			watchdog_process_id=$1
			if [ -r "/proc/$watchdog_process_id/stat" ]; then
				IFS= read -r watchdog_process_stat <"/proc/$watchdog_process_id/stat" || return 1
				watchdog_process_stat=${watchdog_process_stat##*) }
				set -- $watchdog_process_stat
				[ -n "${20:-}" ] || return 1
				printf "%s\\n" "${20}"
				return 0
			fi
			watchdog_process_start=$(ps -p "$watchdog_process_id" -o lstart= 2>/dev/null | tr -d "[:space:]")
			[ -n "$watchdog_process_start" ] || return 1
			printf "%s\\n" "$watchdog_process_start"
		}
		stop_supervisor_watchdog() {
			if [ -n "${supervisor_watchdog_pid:-}" ]; then
				watchdog_attempt=0
				while [ ! -s "$launch_marker.watchdog" ] && [ "$watchdog_attempt" -lt 30 ]; do
					watchdog_attempt=$((watchdog_attempt + 1))
					sleep 0.1
				done
				watchdog_sleep_pid=$(sed -n "1p" "$launch_marker.watchdog" 2>/dev/null || true)
				watchdog_sleep_identity=$(sed -n "2p" "$launch_marker.watchdog" 2>/dev/null || true)
				if [ -n "$watchdog_sleep_pid" ] && [ -n "$watchdog_sleep_identity" ] &&
					[ "$(watchdog_process_identity "$watchdog_sleep_pid" || true)" = "$watchdog_sleep_identity" ]; then
					kill -KILL "$watchdog_sleep_pid" 2>/dev/null || true
				fi
				kill -TERM "$supervisor_watchdog_pid" 2>/dev/null || true
				wait "$supervisor_watchdog_pid" 2>/dev/null || true
			fi
			rm -f "$launch_marker.watchdog"
		}
		supervisor_cleanup() {
			stop_supervisor_watchdog
			[ -z "${stderr_forward_pid:-}" ] || {
				kill -TERM "$stderr_forward_pid" 2>/dev/null || true
				wait "$stderr_forward_pid" 2>/dev/null || true
			}
		}
		trap supervisor_cleanup EXIT
		if [ "$stderr_capture" -eq 1 ]; then
			tee "$launch_marker.stderr" <"$launch_marker.stderr.pipe" >&2 8>&- &
		else
			tee <"$launch_marker.stderr.pipe" >&2 8>&- &
		fi
		stderr_forward_pid=$!
		exec 8<>"$launch_marker.lease" || exit 125
		(
			exec 7<"$launch_marker.lease" || exit 125
			: >"$launch_marker.lease-ready" || exit 125
			while IFS= read -r lease_line; do :; done <&7
		) 8>&- &
		lease_reader_pid=$!
		lease_reader_attempt=0
		while [ ! -e "$launch_marker.lease-ready" ]; do
			kill -0 "$lease_reader_pid" 2>/dev/null || exit 125
			[ "$lease_reader_attempt" -lt 300 ] || exit 125
			lease_reader_attempt=$((lease_reader_attempt + 1))
			sleep 0.1
		done
		(umask 077 && : >"$launch_marker.ready") || exit 125
		while [ ! -e "$launch_marker.go" ]; do
			[ ! -e "$launch_marker.cancel" ] || exit 125
			sleep 1
		done
		"$@" 2>"$launch_marker.stderr.pipe"
		supervisor_status=$?
		exec 8>&-
			(
				watchdog_sleep_pid=
				watchdog_stop() { exit 0; }
				watchdog_cleanup() {
					if [ -n "${watchdog_sleep_pid:-}" ]; then
						watchdog_cleanup_pid=$watchdog_sleep_pid
						watchdog_cleanup_identity=${watchdog_sleep_identity:-}
						watchdog_sleep_pid=
						watchdog_sleep_identity=
						if [ -n "$watchdog_cleanup_identity" ] &&
							[ "$(watchdog_process_identity "$watchdog_cleanup_pid" || true)" = "$watchdog_cleanup_identity" ]; then
							kill -KILL "$watchdog_cleanup_pid" 2>/dev/null || true
						fi
						wait "$watchdog_cleanup_pid" 2>/dev/null || true
					fi
				}
				trap watchdog_cleanup EXIT
				trap watchdog_stop HUP INT TERM
				sleep 10 &
				watchdog_sleep_pid=$!
				watchdog_sleep_identity=$(watchdog_process_identity "$watchdog_sleep_pid" || true)
				[ -n "$watchdog_sleep_identity" ] || exit 125
				printf "%s\\n%s\\n" "$watchdog_sleep_pid" "$watchdog_sleep_identity" >"$launch_marker.watchdog"
				if wait "$watchdog_sleep_pid" 2>/dev/null; then
					watchdog_sleep_pid=
					watchdog_sleep_identity=
				else
					watchdog_sleep_pid=
					watchdog_sleep_identity=
					exit 0
				fi
			(umask 077 && : >"$launch_marker.escaped") 2>/dev/null || true
			kill -TERM "$lease_reader_pid" 2>/dev/null || true
			kill -TERM "$stderr_forward_pid" 2>/dev/null || true
		) &
		supervisor_watchdog_pid=$!
		wait "$lease_reader_pid" 2>/dev/null || true
		wait "$stderr_forward_pid" 2>/dev/null || true
		stop_supervisor_watchdog
		supervisor_watchdog_pid=
		stderr_forward_pid=
		[ ! -e "$launch_marker.escaped" ] || supervisor_status=125
		(umask 077 && printf "%s\\n" "$supervisor_status" >"$launch_marker.status" &&
			: >"$launch_marker.finished") || exit 125
		read -r supervisor_release <"$launch_marker.wait" || exit 125
		exit "$supervisor_status"
	' dans-operation-supervisor "$running_launch_marker" "$running_launch_capture_stderr" "$@" <&9 &
	running_pid=$!
	exec 9<&-
	launch_attempt=0
	while [ ! -e "$running_launch_marker.ready" ]; do
		if ! kill -0 "$running_pid" 2>/dev/null; then
			wait "$running_pid" 2>/dev/null || true
			clear_supervised_launch
			running_pid=
			return 1
		fi
		if [ "$launch_attempt" -ge 300 ]; then
			cancel_supervised_launch
			return 1
		fi
		launch_attempt=$((launch_attempt + 1))
		sleep 0.1
	done
	running_group_id=$(process_group_id "$running_pid" || true)
	running_identity=$(process_identity "$running_pid" || true)
	if [ -z "$running_group_id" ] || [ -z "$running_identity" ]; then
		cancel_supervised_launch
		return 1
	fi
	if ! process_group_isolated "$running_group_id"; then
		running_group_baseline_file=$(mktemp "$lock_dir/operation-group.XXXXXX") || {
			cancel_supervised_launch
			return 1
		}
		if ! capture_group_baseline "$running_group_id" "$running_group_baseline_file"; then
			cancel_supervised_launch
			return 1
		fi
	fi
	capture_process_tree "$running_pid"
	if ! (umask 077 && : >"$running_launch_marker.go"); then
		cancel_supervised_launch
		return 1
	fi
}

merge_running_records() {
	for observed_record in $1; do
		observed_seen=0
		for tracked_record in $running_records; do
			[ "$tracked_record" = "$observed_record" ] && {
				observed_seen=1
				break
			}
		done
		[ "$observed_seen" -eq 1 ] || running_records="$running_records $observed_record"
	done
}

	wait_supervised() {
		while [ ! -e "$running_launch_marker.finished" ]; do
			process_running "$running_pid" || {
				mark_operation_uncertain
				return 125
			}
		capture_process_tree "$running_pid"
		merge_running_records "$captured_process_records"
		sleep 0.1
	done
	capture_process_tree "$running_pid"
	merge_running_records "$captured_process_records"
	[ ! -e "$running_launch_marker.escaped" ] || {
		mark_operation_uncertain
		return 125
	}
	running_status=$(sed -n '1p' "$running_launch_marker.status" 2>/dev/null || true)
	case "$running_status" in
		''|*[!0-9]*)
			mark_operation_uncertain
			return 125
			;;
		*) ;;
	esac
	if [ "$running_status" -ne 0 ] && [ "$running_launch_capture_stderr" -eq 1 ] &&
		[ -s "$running_launch_marker.stderr" ] &&
		grep -Eiq '(cannot connect to the Docker daemon|error during connect|context deadline exceeded|i/o timeout|tls handshake timeout|server gave HTTP response|daemon.*(unavailable|not responding))' \
		"$running_launch_marker.stderr"; then
		mark_operation_uncertain
		return 125
	fi
	return "$running_status"
}

release_supervised_launch() {
	write_supervised_signal release || return 1
	[ -z "${running_pid:-}" ] || wait "$running_pid" 2>/dev/null || true
}

sanitize_failure() {
	phase=$1
	for file in "$run/$phase.out" "$run/$phase.err"; do
		[ -f "$file" ] || continue
		sed -E \
			-e 's/Console token: .*/Console token: [redacted]/' \
			-e 's/(DANS_API_TOKEN=)[^[:space:]]+/\1[redacted]/g' \
			-e 's/dans_v1_[A-Za-z0-9_-]+/[credential-redacted]/g' \
			"$file" | grep -E '^(dev stack:|host (HTTP|DNS)|DANS API returned|make: \*\*)' || true
	done
}

for command in awk chmod cksum curl dig docker id jq ls make mkdir mkfifo mktemp pgrep ps sed stat tee tr; do
	command -v "$command" >/dev/null 2>&1 || fail "missing required command $command"
done

compose() {
	[ -z "$operation_marker" ] || [ ! -e "$operation_marker" ] || return 125
	pending_signal=0
	launching=1
	set -m
	if [ -t 0 ]; then
		if ! launch_supervised docker compose --project-name "$project" --file "$root/compose.yaml" "$@" </dev/null; then
			launching=0
			mark_operation_uncertain
			return 125
		fi
	else
		exec 3<&0
		if ! launch_supervised docker compose --project-name "$project" --file "$root/compose.yaml" "$@" <&3; then
			exec 3<&-
			launching=0
			mark_operation_uncertain
			return 125
		fi
		exec 3<&-
	fi
	running_records=$captured_process_records
	set +m
	launching=0
	[ "$pending_signal" -eq 0 ] || lifecycle_signal
	if wait_supervised; then
		rc=0
	else
		rc=$?
	fi
	if ! drain_running_processes; then
		mark_operation_uncertain
		operation_ambiguous=1
		cancel_supervised_launch
		running_pid=
		running_group_id=
		running_identity=
		running_records=
		return 125
	fi
	release_supervised_launch || {
		mark_operation_uncertain
		cancel_supervised_launch
		return 125
	}
	[ "$rc" -ge 128 ] && mark_operation_uncertain
	clear_supervised_launch
	running_pid=
	running_group_id=
	running_identity=
	running_records=
	return "$rc"
}

volume_query() {
	volume_output=$1
	volume_error=$2
	pending_signal=0
	launching=1
	set -m
	if ! launch_supervised docker volume ls -q --filter "label=com.docker.compose.project=$project" \
		>"$volume_output" 2>"$volume_error"; then
		launching=0
		mark_operation_uncertain
		return 125
	fi
	running_records=$captured_process_records
	set +m
	launching=0
	[ "$pending_signal" -eq 0 ] || lifecycle_signal
	if wait_supervised; then
		rc=0
	else
		rc=$?
	fi
	if ! drain_running_processes; then
		mark_operation_uncertain
		operation_ambiguous=1
		cancel_supervised_launch
		running_pid=
		running_group_id=
		running_identity=
		running_records=
		return 125
	fi
	release_supervised_launch || {
		mark_operation_uncertain
		cancel_supervised_launch
		return 125
	}
	clear_supervised_launch
	running_pid=
	running_group_id=
	running_identity=
	running_records=
	return "$rc"
}

if ! compose ps -aq >"$run/preflight.containers" 2>"$run/preflight.err"; then
	fail 'could not establish whether the checkout project has containers'
fi
[ ! -s "$run/preflight.containers" ] || {
	fail 'checkout project already has containers'
}
if ! volume_query "$run/preflight.volumes" "$run/preflight-volumes.err"; then
	fail 'could not establish whether the checkout project has volumes'
fi
[ ! -s "$run/preflight.volumes" ] ||
	fail 'checkout project already has volumes'

port_base=$((39000 + ($$ % 1000) * 2))
export DANS_DEV_HTTP_PORT=${DANS_DEV_HTTP_PORT:-$port_base}
export DANS_DEV_DNS_PORT=${DANS_DEV_DNS_PORT:-$((port_base + 1))}

run_make() {
	run_make_count=$((run_make_count + 1))
	run_completion_marker=$run/make-complete-$run_make_count
	rm -f "$run_completion_marker"
	pending_signal=0
	launching=1
	set -m
	if ! launch_supervised sh -c '
		make_root=$1
		shift
		stack_lock=$1
		shift
		project_lock=$1
		shift
		operation_marker=$1
		shift
		completion_marker=$1
		shift
		cd "$make_root" && env -u COMPOSE_PROJECT_NAME DANS_DEV_STACK_LOCK_OWNER="$stack_lock" \
			DANS_DEV_PROJECT_LOCK_HELD=1 DANS_DEV_PROJECT_LOCK_OWNER="$project_lock" \
			DANS_DEV_OPERATION_MARKER="$operation_marker" \
			DANS_DEV_COMPLETION_MARKER="$completion_marker" make "$@"
	' dans-lifecycle-make "$root" "$lock_file" "$project_lock_file" \
		"$run_operation_marker" "$run_completion_marker" "$@"; then
		launching=0
		mark_operation_uncertain
		return 125
	fi
	running_records=$captured_process_records
	set +m
	launching=0
	[ "$pending_signal" -eq 0 ] || lifecycle_signal
	if wait_supervised; then
		rc=0
	else
		rc=$?
	fi
	if ! drain_running_processes; then
		mark_operation_uncertain
		operation_ambiguous=1
		cancel_supervised_launch
		running_pid=
		running_group_id=
		running_identity=
		running_records=
		return 125
	fi
	release_supervised_launch || {
		mark_operation_uncertain
		cancel_supervised_launch
		return 125
	}
	[ -e "$run_operation_marker" ] && operation_ambiguous=1
	[ -e "$run_completion_marker" ] || operation_ambiguous=1
	[ "$rc" -ge 128 ] && operation_ambiguous=1
	clear_supervised_launch
	running_pid=
	running_group_id=
	running_identity=
	running_records=
	return "$rc"
}

claim_ownership() {
	mkdir -p "$root/.dans"
	(umask 077 && printf '%s\n' "$project" >"$ownership_marker")
	chmod 600 "$ownership_marker"
}

clear_ownership() {
	rm -f "$ownership_marker"
	rmdir "$root/.dans" 2>/dev/null || true
	[ ! -e "$root/.dans" ] && [ ! -L "$root/.dans" ] ||
		fail 'lifecycle ownership marker could not be removed'
}

run_phase() {
	phase=$1
	shift
	if "$@" >"$run/$phase.out" 2>"$run/$phase.err"; then
		printf '%s\n' "dev lifecycle: $phase ok"
	else
		status=$?
		printf '%s\n' "dev lifecycle: $phase failed (status $status)" >&2
		sanitize_failure "$phase" >&2
		return "$status"
	fi
}

expect_failure() {
	phase=$1
	expected_status=$2
	expected_message=$3
	shift 3
	if "$@" >"$run/$phase.out" 2>"$run/$phase.err"; then
		printf '%s\n' "dev lifecycle: $phase unexpectedly succeeded" >&2
		return 1
	else
		status=$?
	fi
	[ "$status" -eq "$expected_status" ] || {
		printf '%s\n' "dev lifecycle: $phase exited $status, want $expected_status" >&2
		return 1
	}
	if ! grep -Fq "$expected_message" "$run/$phase.out" "$run/$phase.err"; then
		printf '%s\n' "dev lifecycle: $phase omitted its expected diagnostic" >&2
		return 1
	fi
	printf '%s\n' "dev lifecycle: $phase rejected as expected"
}

read_token() {
	sed -n '1p' "$token_file"
}

save_token() {
	name=$1
	cp "$token_file" "$run/$name.token"
	chmod 600 "$run/$name.token"
}

token_mode() {
	stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1"
}

assert_token() {
	[ -s "$token_file" ] || fail 'operator credential is missing'
	[ "$(token_mode "$token_file")" = 600 ] || fail 'operator credential is not mode 600'
}

assert_identity() {
	file=$1
	jq -e '.handle == "dev-operator" and .enabled == true and .operator == true' \
		"$file" >/dev/null 2>&1 || fail 'operator identity is not the expected enabled operator'
}

assert_operator_identity() {
	file=$1
	expected_id=$2
	jq -e --arg id "$expected_id" \
		'.id == $id and .handle == "dev-operator" and .enabled == true and .operator == true' \
		"$file" >/dev/null 2>&1 || fail 'original operator identity was not preserved'
}

assert_smoke_identity() {
	token=$1
	phase=$2
	run_phase "$phase" cli "$token" identities list
	if ! jq -e --arg id "$identity_id" --arg handle "$identity" \
		'any(.items[]; .id == $id and .handle == $handle and .enabled == true and .operator == false)' \
		"$run/$phase.out" >/dev/null 2>&1; then
		fail 'original smoke identity is missing or changed'
	fi
}

cli() {
	auth_token=$1
	shift
	compose exec -T \
		-e DANS_ENDPOINT=http://127.0.0.1:8080/api/v1 \
		-e DANS_API_TOKEN="$auth_token" -e DANS_OUTPUT=json \
		dans /usr/local/bin/dans "$@"
}

assert_project() {
	[ -s "$project_file" ] || fail 'Compose project ownership was not recorded'
	[ "$(sed -n '1p' "$project_file")" = "$project" ] || fail 'Compose project ownership changed'
}

assert_zone() {
	token=$1
	phase=$2
	run_phase "$phase" cli "$token" zones list
	jq -e --arg zone "$zone" 'any(.[]; .name == $zone)' \
		"$run/$phase.out" >/dev/null 2>&1 || fail 'original DNS zone is missing'
}

assert_audit_count() {
	token=$1
	phase=$2
	run_phase "$phase" cli "$token" audit list --action powerdns.zone.patch --result succeeded
	count=$(jq -er '.items | length' "$run/$phase.out" 2>/dev/null) || fail 'audit history could not be read'
	[ "$count" -ge "$audit_count" ] || fail 'audit history was lost'
	for audit_id in $audit_ids; do
		jq -e --arg id "$audit_id" 'any(.items[]; .id == $id)' \
			"$run/$phase.out" >/dev/null 2>&1 || fail 'original audit event was lost'
	done
}

assert_dns() {
	answer=$(dig @127.0.0.1 -p "$DANS_DEV_DNS_PORT" +time=2 +tries=1 +noall +answer \
		"$allowed" A 2>/dev/null) || fail 'original DNS query failed'
	printf '%s\n' "$answer" | awk '$1 == owner && $4 == "A" && $5 == "192.0.2.10" { found=1 } END { exit found ? 0 : 1 }' owner="$allowed" ||
		fail 'original DNS answer was lost'
}

assert_dns_absent() {
	answer=$(dig @127.0.0.1 -p "$DANS_DEV_DNS_PORT" +time=2 +tries=1 +noall +comments +answer \
		"$allowed" A 2>/dev/null) || fail 'fresh DNS query failed'
	printf '%s\n' "$answer" | grep -Eq 'status: (NOERROR|NXDOMAIN|REFUSED)' ||
		fail 'fresh DNS query returned an unexpected status'
	if printf '%s\n' "$answer" | awk -v owner="$allowed" '$1 == owner && $4 == "A" { found=1 } END { exit found ? 0 : 1 }'; then
		fail 'reset left the original DNS answer behind'
	fi
}

assert_persisted() {
	token=$(read_token)
	run_phase persisted-identity cli "$token" me get
	assert_operator_identity "$run/persisted-identity.out" "$operator_id"
	assert_smoke_identity "$token" persisted-smoke-identity
	assert_zone "$token" persisted-zone
	assert_audit_count "$token" persisted-audit
	assert_dns
}

assert_resources_absent() {
	phase=$1
	if ! remaining_containers=$(compose ps -aq 2>"$run/$phase-containers.err"); then
		fail "could not verify $phase containers"
	fi
	[ -z "$remaining_containers" ] || fail "$phase left containers behind"
	if ! volume_query "$run/$phase-volumes.out" "$run/$phase-volumes.err"; then
		fail "could not verify $phase volumes"
	fi
	remaining_volumes=$(cat "$run/$phase-volumes.out")
	[ -z "$remaining_volumes" ] || fail "$phase left volumes behind"
}

owned=1
claim_ownership
run_phase first-up run_make up
assert_token
assert_project
save_token first
token=$(read_token)
run_phase first-identity cli "$token" me get
assert_identity "$run/first-identity.out"
operator_id=$(jq -er '.id' "$run/first-identity.out" 2>/dev/null) || fail 'first operator identity could not be identified'
printf '%s\n' "$operator_id" >"$run/fixture.operator-id"
run_phase first-status run_make status
run_phase first-smoke run_make smoke
run_phase first-host-smoke run_make smoke-host
zone=$(sed -n 's/^smoke: ok (zone \([^,]*\), identity .*/\1/p' "$run/first-smoke.out")
identity=$(sed -n 's/^smoke: ok (zone [^,]*, identity \([^;]*\);.*/\1/p' "$run/first-smoke.out")
[ -n "$zone" ] || fail 'first smoke did not retain its zone fixture'
[ -n "$identity" ] || fail 'first smoke did not retain its identity fixture'
allowed=allowed.$zone
printf '%s\n' "$zone" >"$run/fixture.zone"
printf '%s\n' "$identity" >"$run/fixture.identity"
run_phase first-identities cli "$token" identities list
identity_id=$(jq -er --arg handle "$identity" \
	'[.items[] | select(.handle == $handle)] | if length == 1 then .[0].id else error end' \
	"$run/first-identities.out" 2>/dev/null) || fail 'first smoke identity could not be identified'
printf '%s\n' "$identity_id" >"$run/fixture.identity-id"
assert_zone "$token" first-zone
run_phase first-audit cli "$token" audit list --action powerdns.zone.patch --result succeeded
audit_count=$(jq -er '.items | length' "$run/first-audit.out" 2>/dev/null) || fail 'first audit history could not be read'
[ "$audit_count" -gt 0 ] || fail 'first smoke did not record an audit event'
audit_ids=$(jq -er --arg actor "$identity_id" \
	'[.items[] | select(.actor_id == $actor) | .id] | if length > 0 then .[] else error end' \
	"$run/first-audit.out" 2>/dev/null) || fail 'first smoke audit event could not be identified'
printf '%s\n' "$audit_ids" >"$run/fixture.audit-ids"
assert_dns

if [ "${DANS_DEV_LIFECYCLE_INJECT_FAILURE:-}" = after-first-up ]; then
	fail 'injected lifecycle assertion failure'
fi

save_token repeated
run_phase repeated-up run_make up
cmp -s "$run/repeated.token" "$token_file" || fail 'repeated startup replaced a valid credential'
assert_persisted

run_phase down run_make down
if ! remaining_containers=$(compose ps -aq 2>"$run/down-containers.err"); then
	fail 'could not verify down containers'
fi
[ -z "$remaining_containers" ] || fail 'down left containers behind'
run_phase restart-up run_make up
cmp -s "$run/repeated.token" "$token_file" || fail 'stop/start replaced a valid credential'
assert_persisted

rm -f "$token_file"
run_phase missing-token-up run_make up
assert_token
token=$(read_token)
run_phase missing-token-identity cli "$token" me get
assert_identity "$run/missing-token-identity.out"
assert_persisted

save_token revoked
run_phase token-list cli "$token" me tokens
token_id=$(jq -er '[.items[] | select((.label // "") | startswith("local-recovery-"))] | sort_by(.created_at) | last | .id' \
	"$run/token-list.out" 2>/dev/null) || fail 'recovered token id could not be identified'
run_phase revoke-token cli "$token" me token-revoke "$token_id"
run_phase revoked-token-up run_make up
assert_token
if cmp -s "$run/revoked.token" "$token_file"; then
	fail 'revoked credential was reused'
fi
token=$(read_token)
run_phase revoked-token-identity cli "$token" me get
assert_identity "$run/revoked-token-identity.out"
expect_failure revoked-token-rejected 1 '401 Unauthorized' cli "$(sed -n '1p' "$run/revoked.token")" me get
assert_persisted

save_token reset
cp "$project_file" "$run/reset.project"
expect_failure reset-without-confirmation 2 'reset deletes local data; rerun with CONFIRM=1' run_make reset CONFIRM=
cmp -s "$run/reset.token" "$token_file" || fail 'unconfirmed reset changed the credential'
cmp -s "$run/reset.project" "$project_file" || fail 'unconfirmed reset changed project ownership'
assert_persisted

run_phase confirmed-reset run_make reset CONFIRM=1
[ ! -e "$root/.dans/dev" ] || fail 'confirmed reset left local state behind'
assert_resources_absent confirmed-reset
clear_ownership
owned=0

owned=1
claim_ownership
run_phase fresh-up run_make up
assert_token
assert_project
if cmp -s "$run/reset.token" "$token_file"; then
	fail 'fresh startup reused the reset credential'
fi
token=$(read_token)
run_phase fresh-identity cli "$token" me get
assert_identity "$run/fresh-identity.out"
fresh_operator_id=$(jq -er '.id' "$run/fresh-identity.out" 2>/dev/null) || fail 'fresh operator identity could not be identified'
[ "$fresh_operator_id" != "$operator_id" ] || fail 'fresh startup reused the old operator identity'
expect_failure fresh-old-token-rejected 1 '401 Unauthorized' cli "$(sed -n '1p' "$run/reset.token")" me get
run_phase fresh-identities cli "$token" identities list
if ! jq -e --arg operator_id "$operator_id" --arg smoke_id "$identity_id" \
	'all(.items[]; .id != $operator_id and .id != $smoke_id)' \
	"$run/fresh-identities.out" >/dev/null 2>&1; then
	fail 'fresh startup retained an old identity'
fi
run_phase fresh-audit cli "$token" audit list --action powerdns.zone.patch --result succeeded
if ! jq -e '.items | type == "array" and length == 0' \
	"$run/fresh-audit.out" >/dev/null 2>&1; then
	fail 'fresh startup retained the old audit history'
fi
run_phase fresh-zones cli "$token" zones list
if ! jq -e --arg zone "$zone" 'type == "array" and all(.[]; .name != $zone)' \
	"$run/fresh-zones.out" >/dev/null 2>&1; then
	fail 'fresh startup saw or could not parse the old database fixture'
fi
assert_dns_absent
run_phase final-reset run_make reset CONFIRM=1
assert_resources_absent final-reset
clear_ownership
owned=0

printf '%s\n' 'dev lifecycle: ok'
