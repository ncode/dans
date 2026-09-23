## 1. Initialization and cached credentials

- [x] 1.1 Add a focused behavior check demonstrating that a cached revoked token currently bypasses validation; include a fresh database with an old local token file so bootstrap/readiness ordering is covered.
- [x] 1.2 Separate the serialized bootstrap attempt from cached-token validation, distinguishing only the known existing-installation conflict from bootstrap failures.
- [x] 1.3 Validate credentials with the existing public `me get` CLI after readiness; require the expected handle and enabled/operator state, preserve valid token bytes, and fail on wrong identities or malformed responses.

## 2. Bounded recovery and file publication

- [x] 2.1 Recover a missing/empty token or exact HTTP 401 rejection through the existing offline operator command, with at most one recovery attempt per startup and no identity/data repair.
- [x] 2.2 Validate bootstrap/recovery candidates before atomic mode-0600 installation under the mode-0700 local directory; preserve prior files on errors and clean only invocation-owned temporary files.
- [x] 2.3 Verify configuration/malformed-token failures, transport failures, 5xx/unexpected responses, disabled/demoted/absent operators, and restore-readiness failures cannot silently rotate credentials or grant authority.
- [x] 2.4 Verify candidate-validation failure, local write failure, and interrupted issuance preserve the previous file and bound subsequent recovery; ensure failures never print candidate secrets.

## 3. Real-stack validation and handoff

- [x] 3.1 Verify valid reuse, missing-token recovery, revoked/expired/unknown credential handling, and fresh-database bootstrap with an old file using disposable fixtures; assert identity/data preservation and absence of unnecessary token issuance.
- [x] 3.2 Verify the revoked token remains rejected, ordinary smoke still succeeds after recovery, and successful startup presents only the validated final credential.
- [x] 3.3 Run focused behavior checks and `make dev-contract`, record sanitized results for the lifecycle CI change, and validate this OpenSpec change strictly before publication review.

## Verification notes

- Three full disposable-stack lifecycle runs passed locally, including credential reuse, all recovery cases, post-recovery smoke, and fresh-database bootstrap with an old token file.
- `make dev-contract`, shell syntax, Compose configuration, and `git diff --check` passed locally.
- The existing Linux development-stack CI job runs the same lifecycle script. `openspec validate recover-stale-dev-credentials --strict` passed.
