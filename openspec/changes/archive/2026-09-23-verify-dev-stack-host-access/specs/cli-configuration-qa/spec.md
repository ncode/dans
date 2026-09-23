## ADDED Requirements

### Requirement: Development smoke offers explicit host access verification
The development workflow SHALL provide `make smoke-host` on macOS and Linux to verify published loopback HTTP and authoritative DNS access using a unique smoke fixture. The command SHALL include the existing delegated-write, denial, and audit checks, preserve their retained-fixture behavior, and report failure if any required check fails. Ordinary startup and `make smoke` MUST retain their Docker Compose v2 and Make prerequisite contract.

#### Scenario: Host tools are installed
- **WHEN** the stack is running and the caller runs `make smoke-host` with the required host tools
- **THEN** the command exercises one unique smoke fixture and reports success only after internal authorization/audit checks and all host probes succeed

#### Scenario: Host tools are unavailable
- **WHEN** a host verification prerequisite is missing
- **THEN** the command fails with a specific prerequisite diagnostic before creating a smoke fixture, while the ordinary Docker-only smoke workflow remains available

### Requirement: Host smoke verifies published HTTP and both DNS transports
Host smoke SHALL verify the expected readiness response and console shell through the published loopback HTTP port and the fixture's exact authoritative DNS answer through the published DNS port over both UDP and TCP. Requests MUST originate from the developer or CI host, honor the existing HTTP/DNS port overrides, bypass HTTP proxies for loopback probes, and use bounded request times. DNS UDP checks MUST NOT fall back to TCP.

#### Scenario: Default ports are reachable
- **WHEN** HTTP and both DNS transports are published on their documented default ports
- **THEN** host smoke verifies HTTP readiness and console delivery plus a successful authoritative DNS response containing the fixture's owner, type, and value over each transport

#### Scenario: Ports are overridden
- **WHEN** the caller uses the existing HTTP and DNS port overrides consistently for startup and smoke
- **THEN** all host probes use the overridden ports and do not probe the defaults

#### Scenario: A published path fails while internal services remain healthy
- **WHEN** the HTTP, UDP DNS, or TCP DNS host publication is independently unavailable while internal smoke still works
- **THEN** host smoke fails within its bounded probe time and identifies the failing protocol and effective port

#### Scenario: An endpoint returns the wrong content
- **WHEN** HTTP returns a generic page instead of the expected readiness/console content or DNS returns a wrong owner, type, value, non-authoritative response, or error status
- **THEN** host smoke fails even if the underlying connection succeeded

#### Scenario: Loopback requests would otherwise use a proxy
- **WHEN** the host has HTTP proxy environment settings
- **THEN** the HTTP probes still contact the published loopback listener directly

### Requirement: Host verification preserves the local exposure boundary
Host verification MUST NOT require publishing the database or upstream management API, broadening listener bindings, reading operator credentials for public HTTP probes, or adding production test endpoints. Diagnostics SHALL identify failed checks without printing credentials or authentication headers.

#### Scenario: A host access check fails
- **WHEN** host smoke reports a failed HTTP or DNS probe
- **THEN** its diagnostic contains only the relevant check and nonsecret result, and the command does not alter network exposure to repair the failure
