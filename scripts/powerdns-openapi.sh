#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
vendor_dir="$script_dir/../api/openapi/vendor"
source_file="$vendor_dir/powerdns-authoritative-5.1.3.yaml"
checksum_file="$vendor_dir/powerdns-authoritative-5.1.3.sha256"
source_url="https://raw.githubusercontent.com/PowerDNS/pdns/auth-5.1.3/docs/http-api/openapi/authoritative-api-openapi.yaml"

digest() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

case "${1:-verify}" in
verify)
	expected=$(awk 'NR == 1 {print $1}' "$checksum_file")
	actual=$(digest "$source_file")
	if [ "$actual" != "$expected" ]; then
		echo "PowerDNS OpenAPI checksum mismatch: got $actual, want $expected" >&2
		exit 1
	fi
	;;
update)
	temp_file=$(mktemp "${TMPDIR:-/tmp}/dans-powerdns-openapi.XXXXXX")
	trap 'rm -f "$temp_file"' EXIT HUP INT TERM
	curl -fsSL "$source_url" -o "$temp_file"
	actual=$(digest "$temp_file")
	mv "$temp_file" "$source_file"
	printf '%s  %s\n' "$actual" "$(basename "$source_file")" >"$checksum_file"
	trap - EXIT HUP INT TERM
	;;
*)
	echo "usage: $0 [verify|update]" >&2
	exit 2
	;;
esac
