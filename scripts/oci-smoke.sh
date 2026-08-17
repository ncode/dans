#!/bin/sh
set -eu

test "$#" -eq 3 || {
	echo "usage: $0 IMAGE PLATFORM VERSION" >&2
	exit 2
}

image=$1
platform=$2
version=$3
engine=${CONTAINER_ENGINE:-docker}
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository=$(CDPATH= cd -- "$script_dir/.." && pwd)
built=0
container=

cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	if [ -n "$container" ]; then
		"$engine" container rm "$container" >/dev/null 2>&1 || true
	fi
	if [ "$built" -eq 1 ]; then
		"$engine" image rm "$tag" >/dev/null 2>&1 || true
	fi
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

case "$platform" in
linux/amd64 | linux/arm64) ;;
*)
	echo "unsupported smoke platform: $platform" >&2
	exit 2
	;;
esac

tag="${image}-${platform#linux/}"
"$engine" build \
	--file "$repository/Dockerfile" \
	--platform "$platform" \
	--build-arg "VERSION=$version" \
	--tag "$tag" \
	"$repository"
built=1

metadata=$("$engine" image inspect --format '{{.Os}}/{{.Architecture}} {{.Config.User}}' "$tag")
set -- $metadata
test "$1 $2" = "$platform 65532:65532" || {
	echo "unexpected image platform/user: $metadata" >&2
	exit 1
}
container=$("$engine" container create --platform "$platform" "$tag" version)
rootfs_bytes=$("$engine" container inspect --size --format '{{.SizeRootFs}}' "$container")
"$script_dir/footprint.sh" image-rootfs-bytes "$rootfs_bytes" 'OCI root filesystem'
printf '%s\n' "oci-smoke: $platform root filesystem $rootfs_bytes bytes"

actual_version=$("$engine" run --rm --platform "$platform" "$tag" version)
test "$actual_version" = "$version" || {
	echo "unexpected image version: $actual_version" >&2
	exit 1
}
"$engine" run --rm --platform "$platform" "$tag" help >/dev/null
"$engine" run --rm --platform "$platform" "$tag" db migrate --help >/dev/null
