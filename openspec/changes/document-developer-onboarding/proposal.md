## Why

The README gets an evaluator to a running stack, but contributors must inspect scripts to discover integration prerequisites and recovery behavior. Startup failures and token problems need a short, actionable troubleshooting path.

## What Changes

- Add repository acquisition and working-directory guidance before the quickstart, retaining macOS/Linux and Docker Compose v2 plus Make as the evaluator path.
- Distinguish prerequisites for quickstart, optional host probes, native Go checks, frontend work, and full integration tests.
- Document startup/port diagnostics, persisted state, safe stop/start, token recovery limits, and explicitly destructive reset.
- Keep `make help` as the command reference and link to existing deployment and operations documentation for production procedures.
- Walk through the instructions against the implemented commands and record verification limits without adding documentation-string tests.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This is a documentation-only change with `skip_specs: true`; it describes existing behavior and the separately proposed improvements rather than changing runtime requirements.

## Impact

Primarily `README.md`, with small cross-reference or clarification edits to `docs/cli.md` and existing operations documentation only where needed. No code, configuration behavior, API, or dependency changes.

## Dependencies and Scope

Finalize the host-probe and credential-recovery instructions after [host access verification](../verify-dev-stack-host-access/proposal.md) and [stale credential recovery](../recover-stale-dev-credentials/proposal.md) land. Describe CI coverage only after [lifecycle CI](../verify-dev-stack-lifecycle-ci/proposal.md) exists. New platform support, a separate documentation site, and duplication of production runbooks are outside scope.
