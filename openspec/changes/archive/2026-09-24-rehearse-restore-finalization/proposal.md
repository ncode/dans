## Why

The restore runbooks describe a critical credential boundary, but CI does not rehearse a real PostgreSQL backup and restore through the released maintenance command. A database copy retains its tokens and cannot be recognized automatically as a restore, so operators need a tested, explicit finalization sequence before admitting traffic.

## What Changes

- Add a disposable, real-PostgreSQL restore rehearsal to the required integration gate for both supported PostgreSQL majors.
- Verify restored data and operator identity survive, every pre-restore credential is rejected after finalization, and the one replacement credential works.
- Keep the restored DANS instance stopped until finalization, then verify readiness and public API access.
- Update the recovery runbook with the exercised order and privacy-safe verification evidence.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-configuration-qa`: Require CI to exercise the supported backup, restore, and offline finalization procedure against a disposable installation.

## Impact

The integration harness and CI contract, restore/upgrade documentation, and OpenSpec QA contract change. The production API, database schema, release artifact, and deployment configuration do not change. Automatic detection of a PostgreSQL clone remains out of scope.
