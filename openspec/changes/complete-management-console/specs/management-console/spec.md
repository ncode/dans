## Purpose

Let DANS operators and authenticated identities complete access management, credential, recovery, and audit workflows through the embedded console.

## ADDED Requirements

### Requirement: Operator identity and group workflows
The console SHALL let DANS operators list, create, inspect, and update identities and groups and manage direct group membership through the public API. Identity kind and handle and group handle SHALL remain immutable; lifecycle controls SHALL disable or re-enable resources rather than delete them. The console SHALL preserve current server authorization and last-enabled-operator protection.

#### Scenario: Administer an identity and its memberships
- **WHEN** a DANS operator creates a user or service identity and assigns it to a group
- **THEN** the identity, group membership, profile, and token metadata are inspectable in the console
- **AND** token creation requires a separate explicit action

#### Scenario: Reject stale operator authority
- **WHEN** the caller loses operator authority while an administration page is open
- **THEN** subsequent operator-only actions are rejected by the API and the console explains the loss of access
- **AND** the console does not retry mutations or imply that disabled controls enforce authorization

### Requirement: Current authority and retained assignments
Identity details SHALL distinguish effective authority now from retained assignments, including direct and group provenance, and SHALL display operator authority explicitly. Disabled identities SHALL have no usable authority. Disabled groups SHALL contribute no effective authority while retaining visible membership and assignments. Re-enable confirmations SHALL explain that retained access can become usable again; retired zone-lifetime grants SHALL never be represented as recoverable authority.

#### Scenario: Inspect and re-enable a disabled identity
- **WHEN** an operator inspects a disabled identity that retains memberships, tokens, and delegations
- **THEN** the console shows no usable authority and separately exposes retained assignments and token metadata
- **AND** re-enabling requires confirmation explaining restoration of eligible access

#### Scenario: Explain group and operator authority
- **WHEN** an operator inspects access contributed by an enabled group or the DANS operator role
- **THEN** the console identifies the source rather than presenting an unexplained flat delegation list
- **AND** disabled-group assignments remain inspectable without counting as effective authority

### Requirement: Self-service and token interaction
Every enabled identity SHALL have console access to its own read-only profile, memberships, effective authority, and token list/create/revoke operations. Token creation SHALL accept a label and optional expiry and reveal the returned secret once with a Copy action. Token secrets SHALL NOT enter browser persistent storage, logs, URLs, or subsequent metadata views. The token backing the current sign-in SHALL be marked. Self-revocation, self-disablement, and self-demotion SHALL require explicit consequence confirmation, subject to existing API permissions and operator protection.

#### Scenario: Issue a token without implicit credentials
- **WHEN** an authorized caller explicitly creates a token
- **THEN** the console displays its secret once and offers Copy
- **AND** dismissing the secret view leaves only metadata available
- **AND** uncertain issuance is not automatically resubmitted

#### Scenario: Revoke the token backing the browser session
- **WHEN** the caller confirms revocation of the marked sign-in token
- **THEN** the console explains that sessions backed by that token end and transitions to sign-in when authentication is no longer valid
- **AND** unrelated tokens are not revoked

### Requirement: Bounded management browsing and selection
Every management list and identity/group selector SHALL use bounded server-side pagination, including collections used to assign memberships and delegations. Identity and group search SHALL support literal handle prefixes across the complete matching collection, not only loaded rows. Changing filters SHALL reset pagination. Loading, empty, error, disabled, and permission-limited states SHALL remain usable with keyboard navigation and at narrow widths and 200 percent zoom.

#### Scenario: Select a match beyond the first page
- **WHEN** an operator searches for an identity or group whose match is beyond the initially loaded page
- **THEN** server-side search and pagination make that resource selectable without downloading the entire collection

#### Scenario: Validate synthetic management capacity
- **WHEN** the console is exercised with 1,000 identities, 100 groups, 1,000 members in a group, 100 tokens per identity, and 100,000 audit events
- **THEN** lists, relationship views, and selectors remain paginated and audit export retains bounded memory
- **AND** these validation targets are not enforced as product limits

### Requirement: Guided zone binding recovery
The console SHALL let operators list and inspect bindings, bind eligible current zones, explicitly observe upstream state, and perform eligible confirm-absent, retry-delete, and rebind operations. It SHALL explain action consequences and require confirmation for mutations. Observation SHALL NOT silently perform recovery. Unknown outcomes SHALL remain distinguishable from success and failure, with no automatic mutation retries.

#### Scenario: Recover a retired lifetime
- **WHEN** an operator inspects a retired binding after failed or uncertain deletion
- **THEN** the console shows its known state and an explicit upstream observation action
- **AND** eligible recovery actions explain that the old binding remains retired and old delegations are not restored
- **AND** choosing retry-delete submits one new explicitly confirmed mutation

### Requirement: Filtered audit download
The console SHALL expose existing actor, action, target type, target ID, and result filters and offer all matching audit events as a streamed NDJSON download. It SHALL identify the export as a traversal of live history, not a cross-page snapshot. The browser SHALL NOT accumulate the entire export in application memory or report a download complete merely because it has started. Interrupted transfers SHALL fail visibly.

#### Scenario: Download filtered history
- **WHEN** a DANS operator exports the current filters
- **THEN** the download includes matching pages without a hidden page or row cap
- **AND** the download uses ordinary browser completion/failure handling or an equally explicit verified completion mechanism
