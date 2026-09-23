## ADDED Requirements

### Requirement: CI verifies the development quickstart lifecycle
For pull requests and pushes, CI SHALL run the development documentation contract and the root development-stack workflow on Linux with failure propagation and an explicit execution timeout. The check SHALL exercise the documented Make commands against a fresh disposable installation in addition to the existing real-system integration gate.

#### Scenario: First startup and smoke succeed
- **WHEN** the check starts from a fresh checkout without local development state
- **THEN** startup produces a ready installation and usable local operator access, and both delegated-write smoke and host HTTP/DNS verification succeed

#### Scenario: Repeated startup preserves a working installation
- **WHEN** startup is repeated on the same healthy installation
- **THEN** the check verifies unchanged operator credentials and identity, preserved fixture data, and successful readiness without duplicate initialization

#### Scenario: Stop and start preserve both data stores
- **WHEN** the check stops the stack and starts it again
- **THEN** the original database fixture, audit history, authoritative DNS record, and local credential remain usable

### Requirement: CI verifies development credential recovery and reset boundaries
The development lifecycle check SHALL verify missing and rejected credential recovery, explicit reset confirmation, removal of the entire disposable installation on confirmed reset, and successful initialization afterward.

#### Scenario: Missing credential is recovered without resetting data
- **WHEN** the check removes its own local operator token file and starts the initialized stack
- **THEN** a usable replacement is installed while the existing operator identity and fixture data are preserved

#### Scenario: Revoked credential is replaced without reactivation
- **WHEN** the check revokes its local operator token and repeats startup
- **THEN** a new credential works, the revoked credential remains rejected, and existing fixtures persist

#### Scenario: Reset without confirmation changes nothing
- **WHEN** the check requests reset without confirmation
- **THEN** reset fails and leaves project metadata, credential bytes, volumes, and fixtures unchanged

#### Scenario: Confirmed reset is complete
- **WHEN** the check confirms reset of its disposable installation
- **THEN** its containers, data volumes, credential, and project metadata are removed, and the next startup creates a usable new installation without the old database or DNS fixtures

### Requirement: Development lifecycle verification is isolated and produces sanitized evidence
The lifecycle check SHALL refuse pre-existing local development state, operate only on resources owned by its disposable run, and attempt scoped cleanup on success and failure without hiding a failing result. Published output and artifacts MUST exclude credential values, secret files, raw startup output, private machine paths, and unrelated operational identifiers.

#### Scenario: A checkout already has development state
- **WHEN** the check is invoked in a checkout containing an existing local project identity or credential
- **THEN** it fails before mutating that installation and identifies the need for a disposable checkout

#### Scenario: Verification fails after startup
- **WHEN** a lifecycle assertion fails after test resources have been created
- **THEN** the check remains failed, attempts to remove only its owned resources, and emits a sanitized phase/result diagnostic

#### Scenario: Startup emits a sign-in secret
- **WHEN** the lifecycle check captures startup output containing the generated operator token
- **THEN** the token and raw output are withheld from CI logs and uploaded artifacts on both success and failure
