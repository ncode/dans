## ADDED Requirements

### Requirement: Operator identity relationship reads
DANS SHALL expose operator-only, cursor-paginated reads of another identity's memberships, effective delegations, and retained delegation assignments. Responses SHALL identify direct or group provenance and the states needed to explain suspended authority. Effective delegation reads for disabled targets SHALL be empty; retained views SHALL preserve inspectable assignments without granting authority. Operator authority SHALL remain explicit in the identity representation rather than synthesized as a delegation.

#### Scenario: Inspect suspended access
- **WHEN** an enabled DANS operator reads relationships of a disabled identity or its disabled groups
- **THEN** retained memberships and assignments remain visible with their suspension state
- **AND** they do not appear as currently usable authority

#### Scenario: Protect another identity's relationships
- **WHEN** a non-operator requests another identity's management relationships
- **THEN** DANS rejects the request with HTTP 403 and returns no relationship data
- **AND** existing self-service access remains limited to the caller

### Requirement: Current credential metadata
DANS SHALL expose the authenticated caller's current API-token identifier through a self-service metadata read. A browser session SHALL identify its backing token. The response SHALL contain no token secret, digest, browser-session secret, or recoverable credential material and SHALL follow existing no-store and current-authentication rules.

#### Scenario: Identify a browser sign-in token
- **WHEN** an authenticated browser requests current credential metadata
- **THEN** the response identifies the API token backing that browser session
- **AND** another identity's credential cannot be selected through request input

### Requirement: Literal handle-prefix filtering
DANS SHALL add optional literal handle-prefix filtering to identity and group collections and relevant membership collections while preserving exact-handle filters, existing ordering, default and maximum page sizes, and unfiltered visibility. Prefixes SHALL follow the applicable lowercase handle alphabet; punctuation SHALL be matched literally rather than interpreted as a wildcard. Cursors SHALL bind the collection, parent resource, and filters and SHALL NOT be reusable under different search criteria.

#### Scenario: Search a literal prefix
- **WHEN** a caller supplies a valid handle prefix containing underscore, hyphen, or dot
- **THEN** DANS returns only literal prefix matches under the existing visibility and pagination rules
- **AND** malformed prefixes are rejected rather than becoming wildcard searches

#### Scenario: Change search while continuing a collection
- **WHEN** a caller reuses a cursor with a different prefix or parent resource
- **THEN** DANS rejects the cursor with the existing HTTP 422 invalid-input response
- **AND** the caller can restart from the first page with the new filter
