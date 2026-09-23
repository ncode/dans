#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/dans-dev-lifecycle-init.XXXXXX")
cleanup() {
	rm -rf "$work"
}
trap cleanup EXIT

fake_root=$work/repo
mkdir -p "$fake_root/scripts" "$work/bin"
cp "$root/scripts/dev-stack_lifecycle_test.sh" "$fake_root/scripts/dev-stack_lifecycle_test.sh"
chmod 755 "$fake_root/scripts/dev-stack_lifecycle_test.sh"
: >"$fake_root/compose.yaml"

cat >"$work/bin/docker" <<'STUB'
#!/bin/sh
exit 0
STUB
chmod 755 "$work/bin/docker"

cat >"$work/bin/ps" <<'STUB'
#!/bin/sh
set -eu
pid=
for arg do
	case "$arg" in
		[0-9]*) pid=$arg ;;
		lstart=)
			printf 'pid-%s\n' "$pid"
			exit 0
			;;
		pgid=)
			printf '%s\n' "$pid"
			exit 0
			;;
		ppid=)
			if [ "$pid" = "${DANS_DEV_LOCK_WRAPPER_PID:-}" ] &&
				[ -n "${DANS_DEV_LOCK_PARENT_WRAPPER_PID:-}" ]; then
				printf '%s\n' "$DANS_DEV_LOCK_PARENT_WRAPPER_PID"
				exit 0
			fi
			;;
	esac
done
exec /bin/ps "$@"
STUB
chmod 755 "$work/bin/ps"

fake_project_root=$(CDPATH= cd -P -- "$fake_root" && pwd)
fake_project_id=$(printf '%s\n' "$fake_project_root" | cksum | awk '{print $1}')
fake_user_id=$(id -u)
export DEV_TEST_REAL_PGREP=$(command -v pgrep)
lock_dir=/tmp/dans-dev-locks-$fake_user_id
stack_lock=$lock_dir/dans-dev-stack-$fake_project_id.lock.lockdir
project_lock=$lock_dir/dans-dev-compose-dans-dev-$fake_user_id-$fake_project_id.lock.lockdir

cat >"$work/bin/mktemp" <<'STUB'
#!/bin/sh
set -eu
if [ "${1:-}" = -d ]; then
		[ -e "${DEV_TEST_STACK_LOCK:?}/pid" ] &&
			[ -e "${DEV_TEST_PROJECT_LOCK:?}/pid" ] || exit 2
		printf '%s\n' locks-present >"${DEV_TEST_MKTEMP_REACHED:?}"
		[ "${DEV_TEST_MKTEMP_FAIL:-0}" = 1 ] && exit 1
fi
exec /usr/bin/mktemp "$@"
STUB
chmod 755 "$work/bin/mktemp"

cat >"$work/bin/chmod" <<'STUB'
#!/bin/sh
set -eu
if [ "${DEV_TEST_CHMOD_FAIL:-0}" = 1 ] &&
	case "${2:-}" in
		*/dans-dev-lifecycle.*) true ;;
		*) false ;;
	esac
then
	printf '%s\n' "${2:?}" >"${DEV_TEST_CHMOD_REACHED:?}"
	exit 1
fi
exec /bin/chmod "$@"
STUB
chmod 755 "$work/bin/chmod"

cat >"$work/bin/pgrep" <<'STUB'
#!/bin/sh
set -eu
if [ "${1:-}" = -P ]; then
	exit 1
fi
if [ "${1:-}" = -g ] &&
	[ "${2:-}" = "${DANS_DEV_LOCK_WRAPPER_PID:-}" ] &&
	[ -n "${DANS_DEV_LOCK_WRAPPER_PID:-}" ]; then
	printf '%s\n' "$DANS_DEV_LOCK_WRAPPER_PID"
	exit 0
fi
if [ "${1:-}" = -g ]; then
	exit 1
fi
exec "$DEV_TEST_REAL_PGREP" "$@"
STUB
chmod 755 "$work/bin/pgrep"

if TMPDIR=$work/missing PATH="$work/bin:$PATH" \
	DEV_TEST_MKTEMP_FAIL=1 \
	DEV_TEST_MKTEMP_REACHED=$work/mktemp-reached \
	DEV_TEST_STACK_LOCK=$stack_lock \
	DEV_TEST_PROJECT_LOCK=$project_lock \
	"$fake_root/scripts/dev-stack_lifecycle_test.sh" >"$work/stdout" 2>"$work/stderr"; then
	status=0
else
	status=$?
fi
[ "$status" -eq 1 ] || {
	printf '%s\n' "dev lifecycle init: exited $status, want 1" >&2
	exit 1
}
[ ! -e "$stack_lock" ] && [ ! -e "$project_lock" ] || {
	printf '%s\n' 'dev lifecycle init: failed initialization retained a lock' >&2
	exit 1
}
[ "$(sed -n '1p' "$work/mktemp-reached")" = locks-present ] || {
	printf '%s\n' 'dev lifecycle init: allocation failure did not occur after both locks' >&2
	exit 1
}

mkdir "$work/valid-tmp"
if TMPDIR=$work/valid-tmp PATH="$work/bin:$PATH" \
	DEV_TEST_MKTEMP_FAIL=0 \
	DEV_TEST_CHMOD_FAIL=1 \
	DEV_TEST_CHMOD_REACHED=$work/chmod-reached \
	DEV_TEST_MKTEMP_REACHED=$work/mktemp-reached-chmod \
	DEV_TEST_STACK_LOCK=$stack_lock \
	DEV_TEST_PROJECT_LOCK=$project_lock \
	"$fake_root/scripts/dev-stack_lifecycle_test.sh" >"$work/chmod.stdout" 2>"$work/chmod.stderr"; then
	chmod_status=0
else
	chmod_status=$?
fi
[ "$chmod_status" -eq 1 ] || {
	printf '%s\n' "dev lifecycle init: chmod failure exited $chmod_status, want 1" >&2
	exit 1
}
[ ! -e "$stack_lock" ] && [ ! -e "$project_lock" ] || {
	printf '%s\n' 'dev lifecycle init: chmod failure retained a lock' >&2
	exit 1
}
chmod_run=$(sed -n '1p' "$work/chmod-reached")
[ -n "$chmod_run" ] && [ ! -e "$chmod_run" ] || {
	printf '%s\n' 'dev lifecycle init: chmod failure left the run directory behind' >&2
	exit 1
}

printf '%s\n' 'dev lifecycle init: ok'
