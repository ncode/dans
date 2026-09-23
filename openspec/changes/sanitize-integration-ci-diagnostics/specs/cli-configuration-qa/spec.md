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
