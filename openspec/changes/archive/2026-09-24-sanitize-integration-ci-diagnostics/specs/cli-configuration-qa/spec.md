## ADDED Requirements

### Requirement: Integration CI failure evidence is privacy-safe
The real-system integration gate SHALL retain its nonzero result on failure and publish only an allowlisted summary of the matrix leg, execution phase, and outcome. Its CI-visible output and uploaded failure artifact MUST NOT contain raw container logs, process listings, response bodies, credentials, private paths, or unrelated operational identifiers. Raw diagnostics MAY remain in runner-local temporary storage but MUST NOT be uploaded.

#### Scenario: Failure after services start
- **WHEN** an integration assertion fails after test services have started
- **THEN** the job fails and its published summary identifies the matrix leg and last known phase without copying raw diagnostics

#### Scenario: Raw diagnostics contain confidential values
- **WHEN** captured diagnostics include a synthetic credential, machine path, or operational identifier
- **THEN** none of those values appears in the integration gate's CI-visible output or uploaded failure artifact

#### Scenario: Failure before a phase is recorded
- **WHEN** the integration command exits before a phase marker is available
- **THEN** the job still fails and publishes a fixed, nonsecret unknown-phase summary

#### Scenario: Successful integration
- **WHEN** the integration gate succeeds
- **THEN** no failure-diagnostics artifact is uploaded and the existing performance measurements remain available

### Requirement: Development shell CI failures remain diagnosable and bounded
The macOS development-shell contract SHALL emit only fixed, nonsecret phase markers as it advances. Its launcher-supervisor scenario SHALL fail within a test-local bound if the watchdog does not release its readers. Published diagnostics MUST NOT include raw process output, credentials, private paths, or unrelated operational identifiers.

#### Scenario: The watchdog's macOS process lookup misses a new child
- **WHEN** the watchdog cannot read its direct child's start time immediately after spawning it
- **THEN** it still releases the supervisor's readers after its existing deadline, and the launcher scenario completes within its own bound with only a fixed phase diagnostic
