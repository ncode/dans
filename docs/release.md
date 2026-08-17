# Release artifacts

One release version produces two static Linux executables and one checksum manifest:

```sh
scripts/release.sh build v1.0.0 dist/v1.0.0
scripts/release.sh verify dist/v1.0.0
```

The filenames are `dans_VERSION_linux_amd64`, `dans_VERSION_linux_arm64`, and `SHA256SUMS`. Builds disable CGO and VCS metadata, trim source paths, clear the Go build ID, and inject the displayed version. `scripts/release_test.sh` builds twice, compares the artifacts byte-for-byte, verifies both embedded target architectures, and proves that verification rejects a modified or larger-than-24-MiB executable.

Pushing a `v*` tag runs `.github/workflows/release.yml`. The workflow repeats the reproducibility and tamper tests, builds and verifies both executable architectures, publishes all three files to the matching GitHub release, and pushes the production image for `linux/amd64` and `linux/arm64` to GHCR with the version and `latest` tags. An operator verifies the downloaded executable before first use:

```sh
sha256sum --check SHA256SUMS
./dans_v1.0.0_linux_amd64 version
```

Build and publish the same command surface as a multi-platform OCI image:

```sh
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION=v1.0.0 \
  --tag ghcr.io/ncode/dans:v1.0.0 \
  --push .
```

The final image is `scratch`-based, contains only the static executable and CA roots, and defaults to UID/GID 65532 with `dans serve`. Supplying another command runs maintenance or CLI workflows from that same artifact.

Before publishing, exercise each platform independently. The smoke check asserts the image platform, numeric non-root user, embedded version, offline help, maintenance command surface, and a 26-MiB unpacked container-root-filesystem ceiling through `SizeRootFs` (rather than backend-dependent packed image `.Size`):

```sh
scripts/oci-smoke.sh dans-release-smoke linux/amd64 v1.0.0
scripts/oci-smoke.sh dans-release-smoke linux/arm64 v1.0.0
```

Release publication does not replace the complete verification baseline in OpenSpec task 12.6; generation, unit, race, static, integration, and contract gates must also pass.

The evidence-backed artifact, startup, ready-idle memory, and representative request-path measurements are recorded in the [performance and resource baseline](../openspec/changes/build-dans-v1/evidence/performance/README.md). CI runs portable and real-PostgreSQL benchmark correctness smoke but deliberately does not hard-gate shared-runner `ns/op` values.
