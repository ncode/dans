## MODIFIED Requirements

### Requirement: Management visibility and self-service
DANS SHALL permit operators to administer all identity, group, membership, and token resources. An enabled non-operator identity SHALL be limited to its own read-only identity view, paginated group memberships, effective delegations, token metadata, and creation or revocation of its own tokens. Effective self-delegations SHALL additionally expose the associated PowerDNS zone identifier and canonical zone name so callers can interpret their authority without accessing operator-only binding resources.

#### Scenario: Inspect the current identity
- **WHEN** an enabled identity requests its self view
- **THEN** DANS returns that identity's public representation
- **AND** its group, effective-delegation, and token collections are available as separate paginated self-service resources

#### Scenario: Rotate a token through self-service
- **WHEN** an enabled identity creates a new token for itself and later revokes one of its own tokens
- **THEN** DANS performs both operations without requiring operator authority
- **AND** the identity cannot create, inspect, or revoke a token belonging to another identity

#### Scenario: Attempt operator-only administration
- **WHEN** a non-operator attempts to create or update an identity or group, change membership, or assign the operator role
- **THEN** DANS rejects the request with HTTP 403
- **AND** no management state changes

#### Scenario: Resolve effective delegation scope
- **WHEN** a non-operator lists effective self-delegations
- **THEN** each item retains its binding ID, selectors, and restrictions and also identifies its associated zone by ID and name
- **AND** this addition does not grant access to operator-only zone-binding operations or change read visibility

### Requirement: API-key authentication security
DANS SHALL continue authenticating API requests from exactly one X-API-Key header whose value matches the supported token format. Protected browser requests SHALL alternatively authenticate using one valid token-backed browser-session cookie under the browser-authentication contract. Except for the explicitly defined sign-in operation that replaces an existing browser session, a request supplying both credential kinds MUST be rejected rather than selecting one implicitly. Missing, duplicated, malformed, unknown, expired, revoked, and disabled-owner credentials MUST receive the same HTTP 401 error shape without revealing credential state, and authenticated responses MUST include Cache-Control no-store.

#### Scenario: Authenticate an active token
- **WHEN** a request supplies exactly one valid active DANS token in X-API-Key and no browser-session credential
- **THEN** DANS evaluates the current identity and authority for that request
- **AND** DANS does not expose the token in its response, audit data, or structured logs

#### Scenario: Reject an unusable credential uniformly
- **WHEN** a protected request supplies neither credential, supplies a credential more than once, supplies ambiguous credentials, or supplies a malformed, unknown, expired, revoked, or disabled-owner credential
- **THEN** DANS responds with HTTP 401 using the same DANS error representation in every case
- **AND** the response gives no indication whether a token, session, or identity exists

#### Scenario: Authenticate a browser session
- **WHEN** a protected request supplies only one valid browser-session credential
- **THEN** DANS evaluates current session, original-token, identity, and authority state for the request
- **AND** cookie authentication does not allow duplicated or malformed X-API-Key headers to be ignored
