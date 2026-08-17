#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
workflow="$root/.github/workflows/release.yml"

test -f "$workflow" || {
	echo "missing automated release workflow: $workflow" >&2
	exit 1
}

require() {
	pattern=$1
	description=$2
	grep -Fq -- "$pattern" "$workflow" || {
		echo "release workflow does not $description" >&2
		exit 1
	}
}

require 'tags:' 'run for version tags'
require '- "v*"' 'select version tags'
require 'contents: write' 'permit GitHub release publication'
require 'packages: write' 'permit GHCR publication'
require 'scripts/release_test.sh' 'run reproducibility and tamper checks'
require 'scripts/release.sh build "$GITHUB_REF_NAME" "$release_dir"' 'build both release architectures'
require 'scripts/release.sh verify "$release_dir"' 'verify release checksums and target architectures'
require 'gh release create "$GITHUB_REF_NAME"' 'create the GitHub release'
require '"$release_dir/SHA256SUMS"' 'publish the checksum manifest'
require '"$release_dir/dans_${GITHUB_REF_NAME}_linux_amd64"' 'publish the Linux amd64 executable'
require '"$release_dir/dans_${GITHUB_REF_NAME}_linux_arm64"' 'publish the Linux arm64 executable'
require 'docker/setup-qemu-action@v4' 'install multi-platform emulation'
require 'docker/setup-buildx-action@v4' 'install the multi-platform image builder'
require 'scripts/oci-smoke.sh dans-release-smoke linux/amd64 "$GITHUB_REF_NAME"' 'verify the amd64 OCI footprint before publication'
require 'scripts/oci-smoke.sh dans-release-smoke linux/arm64 "$GITHUB_REF_NAME"' 'verify the arm64 OCI footprint before publication'
require 'docker/login-action@v4' 'authenticate to GHCR'
require 'registry: ghcr.io' 'select GHCR'
require 'username: ${{ github.actor }}' 'use the workflow actor for GHCR'
require 'password: ${{ secrets.GITHUB_TOKEN }}' 'use the scoped workflow token for GHCR'
require 'docker/build-push-action@v7' 'build and publish the OCI image'
require 'platforms: linux/amd64,linux/arm64' 'publish both supported OCI platforms'
require 'push: true' 'push the OCI image'
require 'VERSION=${{ github.ref_name }}' 'embed the release version in the OCI image'
require 'ghcr.io/${{ github.repository }}:${{ github.ref_name }}' 'publish the versioned OCI tag'
require 'ghcr.io/${{ github.repository }}:latest' 'publish the latest OCI tag'
