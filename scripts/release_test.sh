#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
release="$script_dir/release.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/dans-release-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

version=v0.0.0-test
first="$work/first"
second="$work/second"

"$release" build "$version" "$first"
"$release" verify "$first"
"$release" build "$version" "$second"

for arch in amd64 arm64; do
	artifact="dans_${version}_linux_${arch}"
	test -x "$first/$artifact"
	cmp "$first/$artifact" "$second/$artifact"
	go version -m "$first/$artifact" | grep -Fq 'GOOS=linux'
	go version -m "$first/$artifact" | grep -Fq "GOARCH=$arch"
done
cmp "$first/SHA256SUMS" "$second/SHA256SUMS"

oversized="$work/oversized"
cp -R "$first" "$oversized"
dd if=/dev/zero bs=1048576 count=25 >>"$oversized/dans_${version}_linux_amd64" 2>/dev/null
if output=$("$release" verify "$oversized" 2>&1); then
	echo "release verification accepted an oversized executable" >&2
	exit 1
fi
printf '%s\n' "$output" | grep -Fq 'exceeds 25165824-byte release size budget' || {
	echo "release verification did not report the executable size budget" >&2
	exit 1
}

printf '\ncorrupt' >>"$first/dans_${version}_linux_amd64"
if "$release" verify "$first" >/dev/null 2>&1; then
	echo "release verification accepted a modified executable" >&2
	exit 1
fi
