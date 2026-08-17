## Purpose

Define the externally observable identity, group, operator, and API-token behavior that protects every DANS management operation.

## Requirements

### Requirement: Identity resources
DANS SHALL let operators create, inspect, list, and update human and service identities through the management API. Each identity SHALL expose `id`, `kind`, `handle`, nullable `display_name`, `enabled`, `operator`, `created_at`, and `updated_at`; `id` SHALL be a server-generated lowercase UUIDv4, `kind` SHALL be `user` or `service`, and `id`, `kind`, and `handle` SHALL be immutable. Identity handles MUST contain 1–63 lowercase ASCII characters matching `[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?`.

#### Scenario: Create an identity
- **WHEN** an operator creates an identity with a valid unused handle and a kind of `user` or `service`
- **THEN** DANS returns the created identity with a new resource ID, `enabled` set to true, and `operator` set to false
- **AND** DANS does not create an API token implicitly

#### Scenario: Reject a duplicate handle
- **WHEN** an operator creates an identity whose handle is already reserved by an enabled or disabled identity
- **THEN** DANS rejects the request with HTTP 409 and leaves the existing identity unchanged

#### Scenario: Reject an immutable-field update
- **WHEN** an operator attempts to change an identity's ID, kind, or handle
- **THEN** DANS rejects the request with HTTP 400 and leaves the identity unchanged

### Requirement: Identity lifecycle and operator safety
DANS SHALL allow operators to change only an identity's display name, enabled state, and operator role. Disabling an identity SHALL immediately prevent new authentication and authority decisions while preserving its tokens, memberships, delegations, handle, and history; DANS MUST reject any transition that would leave no enabled operator.

#### Scenario: Disable an identity
- **WHEN** an operator disables an enabled non-operator identity
- **THEN** that identity's tokens no longer authenticate
- **AND** its memberships, delegations, and unrevoked token metadata remain present

#### Scenario: Re-enable an identity
- **WHEN** an operator re-enables a disabled identity
- **THEN** its unexpired and unrevoked tokens and retained authority become usable again

#### Scenario: Protect the last enabled operator
- **WHEN** a requested demotion or disablement would leave DANS without an enabled operator
- **THEN** DANS rejects the request with HTTP 409
- **AND** the existing operator remains enabled and privileged

#### Scenario: Preserve disabled identities
- **WHEN** an operator inspects or lists disabled identities
- **THEN** DANS returns their metadata and disabled state
- **AND** DANS provides no operation that hard-deletes the identities or releases their handles in version 1

### Requirement: Group resources and direct membership
DANS SHALL let operators create, inspect, list, update, and directly populate non-nested groups. Each group SHALL expose `id`, `handle`, nullable `display_name`, `enabled`, `created_at`, and `updated_at`; its ID and handle SHALL be immutable, its handle MUST follow the identity-handle grammar in a separate group uniqueness namespace and remain reserved while disabled, and disabling it SHALL suspend its contribution to effective authority without deleting memberships or delegations.

#### Scenario: Add and remove a direct member
- **WHEN** an operator adds an identity to a group using the group's and identity's resource IDs
- **THEN** the identity appears in the group's paginated member collection
- **AND** repeating the same addition succeeds without creating a duplicate membership
- **WHEN** the operator removes that membership
- **THEN** the identity no longer receives authority through the group
- **AND** repeating the removal succeeds without changing other state

#### Scenario: Reject nested groups
- **WHEN** an operator attempts to add one group as a member of another group
- **THEN** DANS rejects the request with HTTP 400
- **AND** neither group is changed

#### Scenario: Disable a group
- **WHEN** an operator disables a group
- **THEN** its retained delegations contribute no authority to its members at subsequent authorization decision points

#### Scenario: Re-enable a group
- **WHEN** an operator re-enables a disabled group
- **THEN** its retained memberships and active delegations contribute authority again

### Requirement: Management visibility and self-service
DANS SHALL permit operators to administer all identity, group, membership, and token resources. An enabled non-operator identity SHALL be limited to its own read-only identity view, paginated group memberships, effective delegations, token metadata, and creation or revocation of its own tokens.

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

### Requirement: API-token creation and representation
DANS SHALL support multiple named API tokens per identity. A newly created token SHALL use the versioned `dans_v1_` prefix followed by 43 unpadded base64url characters, SHALL return its secret only in the successful creation response, and SHALL expose thereafter only `id`, `identity_id`, `label`, derived `status`, nullable `expires_at`, nullable `revoked_at`, and `created_at` metadata.

#### Scenario: Create a token
- **WHEN** an authorized caller creates a token with a valid unused active label and an omitted or future expiry
- **THEN** DANS returns HTTP 201 with the token metadata and one newly generated secret
- **AND** subsequent reads return the metadata without the secret

#### Scenario: Reject invalid token input
- **WHEN** a caller supplies a label outside the handle grammar or an expiry that is not in the future
- **THEN** DANS rejects the request with HTTP 400
- **AND** no token is created

#### Scenario: Reject a duplicate active token label
- **WHEN** a caller supplies a label already used by an active token for that identity
- **THEN** DANS rejects the request with HTTP 409
- **AND** no token is created

#### Scenario: Omit token-use tracking
- **WHEN** a caller reads token metadata
- **THEN** the representation contains no token digest, secret, or last-used timestamp

### Requirement: API-token expiry and revocation
DANS SHALL derive each token's status as `active`, `expired`, or `revoked`; only an active token owned by an enabled identity SHALL authenticate. Revocation SHALL be irreversible and idempotent, expired and revoked metadata SHALL remain queryable, and DANS SHALL allow a revoked label to be reused for a new active token owned by the same identity.

#### Scenario: Revoke a token
- **WHEN** an authorized caller revokes an active token
- **THEN** subsequent authentication with that secret fails
- **AND** repeating the revocation succeeds without changing other state
- **AND** the token remains visible with status `revoked`

#### Scenario: Reach token expiry
- **WHEN** a token's configured expiry is reached
- **THEN** subsequent authentication with that secret fails
- **AND** the token remains visible with status `expired`

#### Scenario: Attempt to restore a revoked token
- **WHEN** a caller attempts to rename, extend, un-revoke, or otherwise mutate a revoked token
- **THEN** DANS rejects the request
- **AND** the token remains revoked

### Requirement: API-key authentication security
DANS SHALL authenticate API requests from exactly one `X-API-Key` header whose value matches the supported token format. Missing, duplicated, malformed, unknown, expired, revoked, and disabled-owner credentials MUST receive the same HTTP 401 error shape without revealing credential state, and authenticated responses MUST include `Cache-Control: no-store`.

#### Scenario: Authenticate an active token
- **WHEN** a request supplies exactly one valid active DANS token in `X-API-Key`
- **THEN** DANS evaluates the current identity and authority for that request
- **AND** DANS does not expose the token in its response, audit data, or structured logs

#### Scenario: Reject an unusable credential uniformly
- **WHEN** a request omits `X-API-Key`, supplies it more than once, or supplies a malformed, unknown, expired, revoked, or disabled-owner token
- **THEN** DANS responds with HTTP 401 using the same DANS error representation in every case
- **AND** the response gives no indication whether a token or identity exists

### Requirement: Cursor-paginated identity collections
DANS SHALL paginate identity, group, member, token, and self-service collections using `items` and nullable `next_cursor`, with a default limit of 100 and a maximum limit of 500. A cursor MUST grant no authority, and each page MUST re-authenticate and re-authorize the caller against current state.

#### Scenario: Continue a collection
- **WHEN** an authorized caller follows `next_cursor` with the same collection filters
- **THEN** DANS returns the next items in stable descending `created_at` and resource-ID order
- **AND** the response contains no total count

#### Scenario: State changes between pages
- **WHEN** collection state changes after one page is returned and before its cursor is followed
- **THEN** the next page reflects a new request-time view rather than a promised cross-page snapshot
- **AND** current visibility rules still apply to every returned item

#### Scenario: Reject a malformed cursor
- **WHEN** a caller supplies a malformed, oversized, or wrong-resource cursor
- **THEN** DANS responds with HTTP 400 and returns no collection data

### Requirement: Exclusive bootstrap and operator recovery
DANS SHALL provide no network bootstrap endpoint. Its offline bootstrap operation MUST succeed only for an uninitialized installation, atomically create the first human operator and one API token, reveal the token once, and ensure that concurrent bootstrap attempts cannot create multiple initial operators. Its recovery operation MUST only issue a replacement token for an existing enabled operator and MUST NOT create an identity or grant operator authority.

#### Scenario: Bootstrap an empty installation
- **WHEN** an authorized deployment administrator runs bootstrap against an uninitialized installation
- **THEN** DANS creates exactly one enabled human operator and one token
- **AND** reveals that token once
- **AND** later bootstrap attempts are rejected without changing state

#### Scenario: Race bootstrap attempts
- **WHEN** multiple bootstrap attempts run concurrently against one uninitialized installation
- **THEN** exactly one attempt succeeds
- **AND** all others fail without creating additional identities or tokens

#### Scenario: Recover operator access
- **WHEN** an authorized deployment administrator requests recovery for an existing enabled operator
- **THEN** DANS issues one replacement token for that operator and reveals it once
- **AND** leaves the operator's identity and role unchanged

#### Scenario: Reject recovery escalation
- **WHEN** recovery targets a missing, disabled, or non-operator identity
- **THEN** DANS rejects recovery
- **AND** does not create, enable, or promote any identity

### Requirement: Token invalidation after restore
DANS MUST require restore finalization before a restored installation becomes ready for authenticated traffic. Finalization SHALL revoke every token present in the restored state and issue exactly one replacement token for a selected existing enabled operator; no pre-restore token SHALL authenticate afterward.

#### Scenario: Finalize a restored installation
- **WHEN** an authorized deployment administrator finalizes a restored installation and selects an enabled operator
- **THEN** DANS revokes every restored token
- **AND** issues and reveals one replacement token for the selected operator
- **AND** permits the installation to become ready only after those changes succeed

#### Scenario: Try a restored token
- **WHEN** any token contained in the restored data is presented after finalization
- **THEN** DANS rejects it with HTTP 401
