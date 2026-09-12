#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

usage() {
	echo "usage: $0 build VERSION OUTPUT_DIR | verify OUTPUT_DIR" >&2
	exit 2
}

checksum() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$@"
	else
		shasum -a 256 "$@"
	fi
}

verify_checksums() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum -c SHA256SUMS
	else
		shasum -a 256 -c SHA256SUMS
	fi
}

verify() {
	directory=$1
	test -f "$directory/SHA256SUMS" || {
		echo "missing release checksum manifest: $directory/SHA256SUMS" >&2
		exit 1
	}
	lines=$(awk 'END {print NR}' "$directory/SHA256SUMS")
	test "$lines" -eq 2 || {
		echo "release checksum manifest must contain exactly two artifacts" >&2
		exit 1
	}
	for arch in amd64 arm64; do
		count=0
		for artifact in "$directory"/dans_*_linux_"$arch"; do
			test -f "$artifact" || continue
			count=$((count + 1))
			bytes=$(wc -c <"$artifact" | awk '{ print $1 }')
			"$script_dir/footprint.sh" binary-bytes "$bytes" "$artifact"
			go version -m "$artifact" | grep -Fq 'GOOS=linux'
			go version -m "$artifact" | grep -Fq "GOARCH=$arch"
		done
		test "$count" -eq 1 || {
			echo "release must contain exactly one linux/$arch executable" >&2
			exit 1
		}
	done
	(
		cd "$directory"
		verify_checksums
	)
}

case "${1:-}" in
build)
	test "$#" -eq 3 || usage
	version=$2
	output=$3
	case "$version" in
	"" | *[!A-Za-z0-9.+-]*)
		echo "release version contains unsupported characters" >&2
		exit 2
		;;
	esac
	make --no-print-directory frontend
	temporary=$(mktemp -d "${TMPDIR:-/tmp}/dans-release.XXXXXX")
	trap 'rm -rf "$temporary"' EXIT HUP INT TERM
	for arch in amd64 arm64; do
		artifact="dans_${version}_linux_${arch}"
		CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build \
			-trimpath -buildvcs=false \
			-ldflags "-s -w -buildid= -X main.version=$version" \
			-o "$temporary/$artifact" ./cmd/dans
	done
	(
		cd "$temporary"
		checksum "dans_${version}_linux_amd64" "dans_${version}_linux_arm64" >SHA256SUMS
	)
	mkdir -p "$output"
	mv "$temporary/dans_${version}_linux_amd64" "$output/"
	mv "$temporary/dans_${version}_linux_arm64" "$output/"
	mv "$temporary/SHA256SUMS" "$output/"
	trap - EXIT HUP INT TERM
	rmdir "$temporary"
	verify "$output"
	;;
verify)
	test "$#" -eq 2 || usage
	verify "$2"
	;;
*)
	usage
	;;
esac
