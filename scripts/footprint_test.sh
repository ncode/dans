#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
footprint="$script_dir/footprint.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/dans-footprint-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

"$footprint" binary-bytes 25165824 release
"$footprint" image-rootfs-bytes 27262976 image
"$footprint" rss-kib 65536 process

reject() {
	metric=$1
	value=$2
	label=$3
	expected=$4
	if output=$("$footprint" "$metric" "$value" "$label" 2>&1); then
		echo "$metric accepted an over-budget value" >&2
		exit 1
	fi
	printf '%s\n' "$output" | grep -Fq "$expected" || {
		echo "$metric did not report its budget" >&2
		exit 1
	}
}

reject binary-bytes 25165825 release '25165824-byte release size budget'
reject image-rootfs-bytes 27262977 image '27262976-byte OCI root filesystem budget'
reject rss-kib 65537 process '65536-KiB ready-idle VmRSS budget'

fake_engine="$work/container-engine"
printf '%s\n' \
	'#!/bin/sh' \
	'case "$1:$2" in' \
	'build:*) exit 0 ;;' \
	'image:inspect) printf "%s\\n" "linux/amd64 65532:65532" ;;' \
	'image:rm) exit 0 ;;' \
	'container:create) printf "%s\\n" fake-container ;;' \
	'container:inspect) printf "%s\\n" 27262977 ;;' \
	'container:rm) exit 0 ;;' \
	'*) exit 2 ;;' \
	'esac' >"$fake_engine"
chmod +x "$fake_engine"

if output=$(CONTAINER_ENGINE="$fake_engine" "$script_dir/oci-smoke.sh" footprint-test linux/amd64 test 2>&1); then
	echo "OCI smoke accepted an oversized image" >&2
	exit 1
fi
printf '%s\n' "$output" | grep -Fq '27262976-byte OCI root filesystem budget' || {
	echo "OCI smoke did not enforce the shared root-filesystem budget" >&2
	exit 1
}
