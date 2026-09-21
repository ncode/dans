## Why

The current smoke command talks to the application and authoritative DNS inside Docker. It can succeed when the loopback HTTP or DNS ports published for developers are unreachable.

## What Changes

- Add `make smoke-host`, an explicit host-side verification of the existing smoke fixture using `curl` and `dig` on macOS and Linux.
- Check HTTP readiness and console delivery through the published HTTP port, plus the fixture's authoritative DNS answer over both UDP and TCP through the published DNS port.
- Honor the existing HTTP/DNS port overrides, bound probe duration, and fail with a targeted diagnostic when a host path or required tool is unavailable.
- Keep `make up` and `make smoke` usable with only Docker Compose v2 and Make; add the optional target and its prerequisites to `make help`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-configuration-qa`: Add explicit verification of the development stack's host HTTP and authoritative DNS paths.

## Impact

Affects `Makefile`, `scripts/dev-stack.sh`, focused smoke checks, and the short command reference. Reuses the existing unique fixture and authorization/audit checks without introducing another stack or exposing PostgreSQL or the upstream management API. Host probe tools are optional for developers and installed explicitly by CI.

## Dependencies and Scope

This change can be implemented independently. [Lifecycle CI](../verify-dev-stack-lifecycle-ci/proposal.md) will run the new target, and [onboarding documentation](../document-developer-onboarding/proposal.md) will explain it. Browser sign-in automation, firewall configuration, public listeners, and additional platform support are outside scope.
