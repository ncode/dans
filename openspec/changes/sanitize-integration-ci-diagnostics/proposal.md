## Why

On integration failure, CI uploads raw container logs and process listings. Those bytes can contain credentials, private paths, and operational identifiers, contrary to the project's publication rules.

## What Changes

- Keep raw integration diagnostics runner-local; do not print or upload them from CI.
- Publish a small allowlisted failure summary that identifies the matrix leg and failing phase without copying untrusted output.
- Add a contract test that injects synthetic secrets and paths into raw diagnostics and verifies they cannot reach the published summary while failure status is preserved.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-configuration-qa`: Require the real-system integration gate to publish only sanitized failure diagnostics.

## Impact

The integration shell harness, its contract test, and the CI workflow change. The public API, runtime behavior, dependencies, and PostgreSQL test matrix do not.
