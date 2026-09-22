## Why

CI exercises the integration Compose stack but never runs the root development stack or `make dev-contract`. A change can therefore pass CI while breaking the documented first-run, restart, recovery, or reset workflow.

## What Changes

- Add a Linux CI job that runs the root Make/Compose workflow from a fresh disposable checkout and executes `make dev-contract`.
- Verify first startup, repeated startup, delegated-write smoke, host access, data preservation across stop/start, missing and stale token recovery, reset refusal, confirmed reset, and subsequent fresh startup.
- Give each run isolated project/credential state, bounded execution, scoped cleanup, and sanitized diagnostics that exclude credentials and raw startup output.
- Exercise the shell boundary on macOS with mocked Docker commands while keeping the full container lifecycle gate on Linux.
- Keep the existing PostgreSQL compatibility integration matrix unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-configuration-qa`: Add a repeatable CI gate for the actual development-stack lifecycle and its persisted state.

## Impact

Affects `.github/workflows/ci.yml`, the existing development contract, and one small lifecycle test harness under `scripts/`. The job uses Docker Compose v2, Make, and the explicitly installed host-probe tools. It adds no application API, schema, or runtime dependency.

## Dependencies and Scope

Complete [host access verification](../verify-dev-stack-host-access/proposal.md) and [stale credential recovery](../recover-stale-dev-credentials/proposal.md) before the full gate is accepted. Those changes own their behavior; this change owns lifecycle orchestration and CI wiring. Hosted macOS container CI and repository branch-protection administration are outside scope; macOS acceptance is limited to the mocked shell-contract job and local Docker verification.
