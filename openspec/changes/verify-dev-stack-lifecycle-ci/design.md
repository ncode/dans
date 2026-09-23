## Context

See [proposal.md](proposal.md) for motivation. The root stack and `integration/compose.yaml` serve different purposes. The development wrapper persists its Compose project identity and operator credential under `.dans/dev/`; deleting volumes without coordinating those files can leave misleading state. Startup currently prints a sign-in token, so its raw output cannot become CI evidence.

## Goals / Non-Goals

**Goals:** Exercise documented commands and verify observable persistent state on a disposable installation; retain a useful, bounded failure signal.

**Non-Goals:** Recreate the full integration matrix, change production behavior, add a test framework, or configure repository branch protection.

## Decisions

### Test the real root workflow in a dedicated Linux job

Add one job to the existing CI workflow for pull requests and pushes, with an explicit timeout and ordinary failure propagation. Run `make dev-contract` and a small shell lifecycle harness. Build through `make up`, using the root Compose file and checked-out code. Install `curl` and `dig` explicitly for `make smoke-host`; no native Go or Node toolchain is needed for the quickstart itself.

The existing integration matrix remains the security/compatibility gate. Reusing only that harness would not test the root startup wrapper; adding another PostgreSQL matrix here would duplicate coverage.

### Use fresh checkout state and assert persistence

The harness requires a disposable checkout with no pre-existing `.dans/dev` state, creates its own Compose resources, and records the resolved project identity. It refuses inherited credential/project state rather than deleting it. A unique temporary checkout is suitable for local invocation; CI uses a job-private checkout. Do not override the wrapper's project identity through environment variables or copy a developer's `.dans` directory.

Run the following acceptance sequence:

1. Start, check readiness and operator access, then run host smoke and retain the fixture's synthetic identifiers privately.
2. Repeat startup and assert the credential bytes, operator identity, and fixture are unchanged.
3. Stop/start and assert database resources, audit history, DNS records, and credential identity persist; query the original DNS fixture, not only a newly created one.
4. Remove only the harness-created token file; start and verify a replacement works while existing fixtures remain.
5. Revoke the current token through the public CLI, start, and verify a replacement works and the revoked token remains unusable.
6. Invoke reset without confirmation and assert a nonzero result plus unchanged credentials, project identity, volumes, and fixtures.
7. Confirm reset; assert that this project's containers/volumes and local credentials/project metadata are absent. Start again, verify a new installation and no old fixture in either database or DNS, then reset the test installation.

Compare secrets in private files or process memory, never by printing them. Recovering credentials and host probes are supplied by the two linked prerequisite changes; this harness assembles them instead of duplicating their logic.

### Use atomic locks and an ownership marker for cleanup

The wrapper and lifecycle harness use portable atomic `mkdir` lock directories
on macOS and Linux, keyed first by the physical checkout path and then by the
persisted Compose project name. Fresh project names include the current user
ID and physical checkout identity; a persisted name must be either that user's
scoped name or the legacy `dans-dev` name, and incomplete token-only legacy
state is reset-only. Checkout locks live in a mode-700 directory owned by the
current user under `/tmp`. Project locks use an atomically created directory in
that user's mode-700 lock directory, so same-user, same-name Compose operations
coordinate without relying on unsafe `O_CREAT` locking. Legacy project names use
a shared `/tmp` lock directory so independently owned legacy checkouts cannot
mutate the same Compose project concurrently; fresh names never need cross-user
coordination because they include the user ID. Fresh project lock directories
are current-user-owned; a pre-existing foreign-owned directory fails closed.
Shared legacy lock directories live directly under `/tmp`; the sticky parent
prevents another user from unlinking a lock they do not own. Lock directories
are removed only after the protected child exits normally. The lock owner PID
is the protected child; a caught interruption terminates and waits for it but
retains both lock directories because a Docker client exit cannot prove that a
daemon-side operation has finished. An uncatchable exit also leaves a stale
directory for manual verification.
The harness writes a mode-600 ownership marker
before its first startup and removes it only after reset and independent
container/volume checks succeed. A workflow cleanup step only reports leftover
state; it never performs an unverified destructive reset.

The Linux job runs the real Docker lifecycle. A separate macOS job first checks
for the standard `mkdir` primitive and then runs the mocked shell tests,
covering the lock and host-probe contracts without claiming that hosted macOS
provides Docker lifecycle coverage.

### Scope cleanup and evidence to the run

Register cleanup before the first startup and keep a workflow cleanup step with `if: always()` as a second opportunity after failure. Resolve project ownership before destructive commands. Cleanup must not replace a failed test's exit status or remove sibling projects. Test the failure path against only harness-owned resources. Hard runner loss can prevent traps, so make no claim that cleanup survives infrastructure loss.

Capture startup output under a restrictive umask and print only allowlisted stage/result summaries. Do not enable shell tracing, upload token files, publish raw Compose configuration, or upload raw startup/container logs. A diagnostic summary can identify the failed phase, expected/actual nonsecret status, and sanitized service health. Scan the publishable summary against credentials held privately before emitting it; on uncertainty emit only the failed phase.

## Risks / Trade-offs

- Multiple startups increase build time → Use normal Docker layer caching and a bounded job timeout; avoid performance thresholds.
- A local test could select existing state → Refuse non-disposable checkouts and verify exact ownership before reset/cleanup.
- Sanitized output gives less detail → Keep stage assertions precise; reproduce failures locally instead of publishing raw evidence.
- Linux CI does not demonstrate macOS Docker forwarding → Record the separate macOS shell-contract coverage and keep full Docker lifecycle verification on Linux.

## Migration Plan

Land host probes and stale credential recovery first, then add this harness/job. No data migration is needed. Reverting the new job and test harness removes this gate without affecting the application or existing integration checks. Whether to require the named check in repository settings is a separate administrative decision.
