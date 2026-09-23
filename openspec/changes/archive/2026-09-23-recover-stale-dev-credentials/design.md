## Context

See [proposal.md](proposal.md). The current `ensure_token` trusts file existence. The compiled CLI already supports `me get`, bootstrap, and recovery for an existing enabled operator. Recovery cannot enable or promote an identity. Readiness also requires initialized installation metadata, so waiting for readiness before handling an empty database would deadlock first startup.

## Goals / Non-Goals

**Goals:** Make successful local startup mean both service readiness and usable access for the expected development operator, preserving valid tokens and data.

**Non-Goals:** Change production authentication, add a public recovery endpoint, introduce host Go/JSON tooling, manage concurrent lifecycle invocations, or automatically repair partially restored installations.

## Decisions

### Separate installation initialization from token validation

After migrations and runtime grants, invoke the existing serialized bootstrap command. A success yields a candidate for a genuinely empty database; its known conflict means an installation already exists and does not trigger immediate recovery. Other bootstrap errors stop startup. This harmless conflict check avoids treating an old cached file as proof that a new database is initialized.

Start the service and wait for its bounded readiness check. For an existing installation, validate the cached token with the in-container CLI's `me get` command. Require a complete response for the configured development handle with enabled and operator status. Preserve valid token bytes and enforce mode 0600. An authenticated but different identity or non-operator is an explicit error, not permission to issue a more privileged credential.

For a fresh installation, validate the bootstrap candidate instead of the old cached file. This handles database replacement without deleting the old file prematurely. Fail closed on malformed response fields; do not treat a loose substring or a successful anonymous readiness check as proof of operator access.

### Recover only after a recognized credential condition

For initialized, ready installations, a missing/empty local file or a well-formed token rejected with HTTP 401 permits one offline recovery attempt for the configured development handle. Reuse the CLI's existing status/diagnostic contract; validate the exact rejection category rather than treating any exit code 1 as an authentication failure. Revoked, expired, and unknown well-formed credentials use the same rejected-credential path.

Configuration errors, malformed tokens, transport failures, 5xx responses, unexpected statuses, and malformed success responses stop startup without recovery. If recovery finds the expected identity disabled, demoted, or absent, it fails without creating or changing identities. Restored-installation readiness failures are not repaired through restore finalization or reset. Newly issued tokens do not reactivate revoked ones.

Validate each bootstrap/recovery candidate through `me get` before publishing it. Allow at most one bootstrap attempt and one recovery attempt per invocation; never loop on token issuance. Keep deadlines and diagnostics bounded.

### Preserve local files until replacement is usable

Keep the candidate in private process state or a mode-0600 temporary file under the mode-0700 credential directory. Once validated, atomically rename the same-directory temporary file into place. On failure, preserve any old token file and remove only temporary files created by the invocation. Database issuance and local file replacement are not one transaction: an interrupted attempt can leave an additional unused database token; do not claim exactly-once issuance or automatically revoke unrelated tokens.

Keep the existing console URL/token presentation only after complete success. Validation diagnostics never include credentials. CI captures that existing success output privately, as specified by the lifecycle CI change.

### Test credential decisions independently of broad integration QA

Add a focused behavior check around the shell command boundary for error classification, identity validation, attempt bounds, candidate verification, and file preservation. Use a disposable real stack to prove valid reuse, missing-token recovery, revocation recovery, and a fresh database paired with an old token file. Simulated errors can cover outages and malformed responses without production hooks. Assert token counts or recovery audit events where needed to detect unintended issuance.

## Risks / Trade-offs

- Error classification depends on the CLI diagnostic contract → Pin classification to HTTP 401 and cover configuration/transport/server errors explicitly.
- Bootstrap conflicts are normal on existing installations → Suppress only the known conflict; preserve a sanitized diagnostic for all other failures.
- Interrupted issuance can leave an unused token → Preserve data and the old local file; document this limit and verify the next invocation remains bounded.

## Migration Plan

No schema or credential format migration. Existing valid token files continue to work unchanged; stale files are replaced only after successful validation. Reverting the wrapper restores previous startup behavior without revoking credentials or changing database data.
