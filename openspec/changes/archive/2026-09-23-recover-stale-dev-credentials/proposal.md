## Why

Startup treats any nonempty local token file as usable. Revocation or replacement of the local database can leave `make up` reporting success with a token that cannot sign in or perform operator actions.

## What Changes

- Validate the cached credential against the running application's public self-identity API before reporting successful startup.
- Preserve a valid credential for the expected enabled development operator; recover rejected credentials only through the existing offline bootstrap/recovery commands against this checkout's database.
- Treat a different identity, lost operator role, disabled identity, malformed token, dependency failure, and malformed API response as explicit failures rather than silently granting authority or rotating on every error.
- Validate a replacement before atomically installing it with restrictive permissions; preserve the old file on unsuccessful recovery and bound recovery attempts.
- Add focused regression coverage for valid reuse, missing/revoked/expired/unknown credentials, fresh databases, and failures that must not rotate credentials.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-configuration-qa`: Require usable local operator credentials at successful development startup and constrain automatic local recovery.

## Impact

Affects `scripts/dev-stack.sh` and its behavior tests. Reuses the existing public self-identity and offline maintenance contracts; no production authentication policy, API, database schema, or new host dependency is required.

## Dependencies and Scope

This change can be implemented independently and is a prerequisite for the recovery scenarios in [lifecycle CI](../verify-dev-stack-lifecycle-ci/proposal.md). It never resets data, enables identities, promotes operators, restores revoked tokens, or applies automatic recovery to production deployments.
