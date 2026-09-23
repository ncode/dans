## 1. Prerequisites and isolated execution

- [x] 1.1 Confirm the host-access and stale-credential changes are implemented and their focused checks pass; map each lifecycle scenario to an observable assertion.
- [x] 1.2 Add one shell lifecycle harness that requires a disposable checkout, refuses existing local state, records its owned project, and establishes private output capture and scoped cleanup before startup.
- [x] 1.3 Verify refusal leaves existing state untouched and an injected harness failure preserves the failure status while cleaning only run-owned resources.

## 2. Lifecycle assertions

- [x] 2.1 Exercise first startup, status, internal smoke, and host smoke; retain original fixture identifiers and credential identity privately for later assertions.
- [x] 2.2 Verify repeated startup and down/up preserve credential bytes, operator identity, database fixture, audit history, and the original authoritative DNS answer.
- [x] 2.3 Exercise missing-token and revoked-token recovery without losing fixtures; verify the old revoked token remains rejected.
- [x] 2.4 Verify reset refusal leaves state unchanged, confirmed reset removes owned volumes and credentials/project metadata, and subsequent fresh startup cannot see the old database or DNS fixture.

## 3. CI and verification

- [x] 3.1 Add a bounded Linux quickstart job to the existing pull-request/push workflow that installs host-probe tools, runs `make dev-contract` and the harness, and attempts cleanup on failure.
- [x] 3.2 Verify generated credentials, private paths, raw startup output, and authentication headers cannot enter success/failure logs or uploaded evidence; publish only the sanitized phase/result summary.
- [x] 3.3 Run the harness on a clean Linux checkout, demonstrate that an intentionally failed lifecycle assertion fails the job, and check that existing integration jobs are unchanged; wire the same full and injected-failure checks into the Linux workflow.
- [x] 3.4 Record sanitized validation results and macOS coverage limits; add the mocked macOS shell-contract job, run strict OpenSpec validation, and review the exact outgoing diff before publication.
