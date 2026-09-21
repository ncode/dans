#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/dans-dev-stack-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

fake_root=$work/repo
mkdir -p "$fake_root/scripts" "$work/bin"
cp "$root/scripts/dev-stack.sh" "$fake_root/scripts/dev-stack.sh"
chmod 755 "$fake_root/scripts/dev-stack.sh"
: >"$fake_root/compose.yaml"

cat >"$work/bin/docker" <<'STUB'
#!/bin/sh
set -eu

mode=${DEV_TEST_MODE:?}
recover_count_file=${DEV_TEST_RECOVER_COUNT_FILE:?}
me_count_file=${DEV_TEST_ME_COUNT_FILE:?}
api_token=
for arg do
	case "$arg" in
		DANS_API_TOKEN=*) api_token=${arg#DANS_API_TOKEN=} ;;
	esac
done

increment() {
	count=$(sed -n '1p' "$1")
	count=$((count + 1))
	printf '%s\n' "$count" >"$1"
}

case "$*" in
	*" bootstrap "*)
		case "$mode" in
			fresh|fresh-invalid|fresh-write-failure)
				printf '%s\n' '{"secret":"candidate"}'
				exit 0
				;;
		esac
		printf '%s\n' 'database: conflict' >&2
		exit 1
		;;
	*" recover operator-token "*)
		increment "$recover_count_file"
		if [ "$mode" = existing-recover-failure ]; then
			printf '%s\n' 'recovery unavailable' >&2
			exit 1
		fi
		printf '%s\n' '{"secret":"recovered"}'
		exit 0
		;;
	*" me get"*)
		increment "$me_count_file"
		case "$mode:$api_token" in
			existing-unauthorized:old|existing-recover-failure:old|existing-recovered-unauthorized:old|existing-recovered-wrong:old)
				printf '%s\n' 'DANS API returned 401 Unauthorized' >&2
				exit 1
				;;
			existing-recovered-unauthorized:recovered)
				printf '%s\n' 'DANS API returned 401 Unauthorized' >&2
				exit 1
				;;
			existing-transport:*)
				printf '%s\n' 'connection refused' >&2
				exit 1
				;;
			existing-unexpected:*)
				printf '%s\n' 'DANS API returned 500 Internal Server Error' >&2
				exit 1
				;;
			existing-malformed:*)
				printf '%s\n' '{"enabled":true,"operator":true}'
				exit 0
				;;
			existing-wrong:*|fresh-invalid:*|existing-recovered-wrong:recovered|missing-recovered-wrong:*)
				printf '%s\n' '{"handle":"other","enabled":true,"operator":true}'
				exit 0
				;;
			existing-disabled:*)
				printf '%s\n' '{"handle":"dev-operator","enabled":false,"operator":true}'
				exit 0
				;;
			existing-nonoperator:*)
				printf '%s\n' '{"handle":"dev-operator","enabled":true,"operator":false}'
				exit 0
				;;
		esac
		printf '%s\n' '{"handle":"dev-operator","enabled":true,"operator":true}'
		exit 0
		;;
esac

exit 0
STUB
chmod 755 "$work/bin/docker"

system_mv=$(command -v mv)
cat >"$work/bin/mv" <<STUB
#!/bin/sh
set -eu
destination=
for arg do
	destination=\$arg
done
case "\${DEV_TEST_BLOCK_TOKEN_MV:-0}:\$destination" in
	1:*/operator-token) exit 1 ;;
esac
exec "$system_mv" "\$@"
STUB
chmod 755 "$work/bin/mv"

cat >"$work/bin/chmod" <<'STUB'
#!/bin/sh
set -eu
destination=
for arg do
	destination=$arg
done
case "${DEV_TEST_BLOCK_TOKEN_CHMOD:-0}:$destination" in
	1:*/operator-token.tmp) exit 1 ;;
esac
exec /bin/chmod "$@"
STUB
chmod 755 "$work/bin/chmod"

run_scenario() {
	name=$1
	expected_status=$2
	expected_token=$3
	token_state=$4
	expected_recover=$5
	expected_me=$6
	fault=$7
	block_mv=0
	block_chmod=0
	case "$fault" in
		mv) block_mv=1 ;;
		chmod) block_chmod=1 ;;
		write|none) ;;
		*) printf '%s\n' "dev stack behavior: unknown fault $fault" >&2; exit 1 ;;
	esac

	rm -rf "$fake_root/.dans"
	mkdir -p "$fake_root/.dans/dev"
	if [ "$token_state" = old ]; then
		printf '%s\n' old >"$fake_root/.dans/dev/operator-token"
	fi
	if [ "$fault" = write ]; then
		# Seed interrupted output, then make the destination a directory target so
		# opening it for the replacement write fails regardless of effective UID.
		printf '%s\n' partial >"$work/partial-credential"
		ln -s / "$fake_root/.dans/dev/operator-token.tmp"
	fi
	recover_count_file=$work/recover-count
	me_count_file=$work/me-count
	printf '%s\n' 0 >"$recover_count_file"
	printf '%s\n' 0 >"$me_count_file"
	stdout=$work/stdout
	stderr=$work/stderr
	if DEV_TEST_MODE=$name \
		DEV_TEST_RECOVER_COUNT_FILE=$recover_count_file \
		DEV_TEST_ME_COUNT_FILE=$me_count_file \
		DEV_TEST_BLOCK_TOKEN_MV=$block_mv \
		DEV_TEST_BLOCK_TOKEN_CHMOD=$block_chmod \
		PATH="$work/bin:$PATH" \
		"$fake_root/scripts/dev-stack.sh" up >"$stdout" 2>"$stderr"; then
		status=0
	else
		status=$?
	fi
	[ "$status" -eq "$expected_status" ] || {
		printf '%s\n' "dev stack behavior: $name exited $status, want $expected_status" >&2
		exit 1
	}
	if [ "$expected_token" = none ]; then
		[ ! -e "$fake_root/.dans/dev/operator-token" ] || {
			printf '%s\n' "dev stack behavior: $name created a token unexpectedly" >&2
			exit 1
		}
	else
		actual_token=$(sed -n '1p' "$fake_root/.dans/dev/operator-token" 2>/dev/null || true)
		[ "$actual_token" = "$expected_token" ] || {
			printf '%s\n' "dev stack behavior: $name changed the cached token unexpectedly" >&2
			exit 1
		}
	fi
	[ "$(sed -n '1p' "$recover_count_file")" -eq "$expected_recover" ] || {
		printf '%s\n' "dev stack behavior: $name recovered an unexpected number of times" >&2
		exit 1
	}
	[ "$(sed -n '1p' "$me_count_file")" -eq "$expected_me" ] || {
		printf '%s\n' "dev stack behavior: $name validated an unexpected number of credentials" >&2
		exit 1
	}
	[ ! -e "$fake_root/.dans/dev/operator-token.tmp" ] || {
		printf '%s\n' "dev stack behavior: $name left a temporary token behind" >&2
		exit 1
	}
}

run_scenario fresh 0 candidate old 0 1 none
run_scenario existing-valid 0 old old 0 1 none
run_scenario existing-unauthorized 0 recovered old 1 2 none
run_scenario existing-missing 0 recovered none 1 1 none
run_scenario existing-recover-failure 1 old old 1 1 none
run_scenario existing-recovered-unauthorized 1 old old 1 2 none
run_scenario existing-recovered-wrong 1 old old 1 2 none
run_scenario missing-recovered-wrong 1 none none 1 1 none
run_scenario existing-wrong 1 old old 0 1 none
run_scenario existing-disabled 1 old old 0 1 none
run_scenario existing-nonoperator 1 old old 0 1 none
run_scenario existing-transport 1 old old 0 1 none
run_scenario existing-unexpected 1 old old 0 1 none
run_scenario existing-malformed 1 old old 0 1 none
run_scenario fresh-invalid 1 old old 0 1 none
run_scenario fresh-write-failure 1 old old 0 1 mv
run_scenario fresh-write-failure 1 old old 0 1 write
run_scenario fresh-write-failure 1 old old 0 1 chmod

printf '%s\n' 'dev stack behavior: ok'
