## ADDED Requirements

### Requirement: Successful development startup verifies operator access
Development startup SHALL report success only after the service is ready and its chosen local credential authenticates as the expected enabled development operator through the public self-identity API. A valid cached credential SHALL be reused without issuing a replacement and SHALL retain restrictive file permissions.

#### Scenario: Cached token is valid for the expected operator
- **WHEN** startup finds a valid local credential for the expected enabled operator
- **THEN** it verifies that identity and operator role, preserves the credential bytes, enforces mode 0600, and completes without issuing another token

#### Scenario: Cached token authenticates as an unexpected identity
- **WHEN** the credential authenticates successfully as a different identity or without the operator role
- **THEN** startup fails with a nonsecret diagnostic and does not promote the identity or replace the credential with privileged access

#### Scenario: Old local credentials remain beside a new empty database
- **WHEN** startup encounters an empty migrated database while an old local token file still exists
- **THEN** it initializes the installation through bootstrap before requiring readiness and replaces the old file only after validating the new operator credential

### Requirement: Automatic development recovery is bounded and preserves authority
For an initialized ready development installation, startup SHALL attempt recovery of a missing/empty local credential or a well-formed credential rejected with HTTP 401 using the existing offline recovery command for the expected development operator. It MUST NOT create, enable, or promote identities during recovery, reactivate revoked tokens, reset data, or finalize a restore. Each invocation SHALL make at most one bootstrap attempt and one recovery attempt.

#### Scenario: Local token file is absent
- **WHEN** an initialized ready installation has no usable token file
- **THEN** startup obtains and validates one replacement for the existing enabled development operator while preserving database and DNS data

#### Scenario: Token is revoked, expired, or unknown
- **WHEN** an initialized ready installation rejects a well-formed cached token with HTTP 401 and the expected development operator remains enabled
- **THEN** startup obtains and validates a replacement, preserves existing data and identity, and leaves the old token rejected

#### Scenario: Expected operator is disabled, demoted, or absent
- **WHEN** offline recovery cannot issue a credential for an existing enabled development operator
- **THEN** startup fails without modifying identity authority or deleting the cached token and does not retry issuance indefinitely

#### Scenario: Dependency or configuration failure occurs
- **WHEN** validation fails because of a malformed cached token in an initialized installation, transport error, server error, unexpected HTTP status, or malformed success response
- **THEN** startup fails without invoking credential recovery or replacing the cached file

#### Scenario: Installation requires restore finalization
- **WHEN** readiness fails because the installation requires an explicit restore workflow
- **THEN** startup reports failure and does not finalize the restore, reset the stack, or treat the readiness failure as token rejection

### Requirement: Development credential replacement is validated and atomic
Startup SHALL validate a newly issued credential before atomically installing it in the local token file. Temporary credential files MUST have mode 0600 in a mode-0700 directory, and unsuccessful validation or replacement MUST preserve any pre-existing token file. Credential-validation diagnostics MUST NOT expose secrets; the existing local console sign-in presentation SHALL occur only after complete startup success.

#### Scenario: Replacement succeeds
- **WHEN** a bootstrap or recovery candidate authenticates as the expected enabled operator and the local file can be replaced
- **THEN** the token file is replaced atomically with mode 0600 and startup reports success

#### Scenario: Candidate validation or file publication fails
- **WHEN** the replacement cannot be validated or installed
- **THEN** startup fails, preserves any prior token file, removes its own temporary credential files where cleanup can run, and does not print the candidate secret

#### Scenario: Issuance is interrupted
- **WHEN** execution stops after database issuance but before local replacement
- **THEN** the previous local file remains intact and the next invocation remains subject to the same bounded recovery rules without revoking unrelated credentials
