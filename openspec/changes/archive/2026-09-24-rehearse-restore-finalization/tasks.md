## 1. Restore boundary contract

- [x] 1.1 Add a focused contract that fails if a restored API service can start before successful offline finalization or if a failing finalization publishes a token, backup content, or raw diagnostic; run it against the current harness to confirm the missing coverage.
- [x] 1.2 Add the isolated restored-database service profile and its scoped cleanup to the existing integration project.

## 2. Real restore rehearsal

- [x] 2.1 Extend each PostgreSQL integration leg to back up a populated installation, restore into a second disposable database, and verify the copied identity, authorization, and audit state without serving the copy.
- [x] 2.2 Finalize the restored copy with the release CLI before starting its API service; verify readiness, same operator identity, preserved data, rejection of captured old credentials, and acceptance of the single replacement credential through the public API.
- [x] 2.3 Keep backup bytes, token values, and raw command output runner-local; preserve failure status and ensure the contract catches finalization failure before service startup.

## 3. Documentation and verification

- [x] 3.1 Update the restore and coordinated-upgrade runbooks with the exercised order and the explicit limit that ordinary PostgreSQL restore cannot be detected automatically.
- [x] 3.2 Run the focused contract, strict OpenSpec validation, and local Docker integration for both supported PostgreSQL majors; record only sanitized results.
- [x] 3.3 Review the exact change with OCR, inspect excluded files locally, fix confirmed findings, and re-run affected checks before publication.
