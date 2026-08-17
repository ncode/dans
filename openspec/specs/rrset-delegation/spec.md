## Purpose

Defines operator-managed, allow-only authority over whole RRsets so identities and groups can safely share a PowerDNS zone without creating delegated DNS subzones.

## Requirements

### Requirement: Immutable delegation resources
DANS SHALL let operators create a delegation with an immutable resource ID, exactly one active zone binding, exactly one identity or group grantee, one or more tagged selectors, an optional non-empty record-type allowlist, and an optional non-empty change-kind allowlist. An omitted record-type allowlist SHALL mean every PowerDNS-supported RR type, including `PTR`; an omitted change-kind allowlist SHALL mean `REPLACE`, `DELETE`, `EXTEND`, and `PRUNE`. DANS MUST expose no operation that edits an existing delegation.

#### Scenario: Create an unrestricted-type delegation
- **WHEN** an operator creates a delegation with one active zone binding, one grantee, at least one valid selector, and no record-type or change-kind allowlist
- **THEN** DANS creates a new immutable delegation that can match every supported RR type and all four supported change kinds

#### Scenario: Invalid delegation shape
- **WHEN** a create request names zero or multiple grantees, has no selector, or supplies an empty allowlist
- **THEN** DANS returns `422 Unprocessable Entity` and creates no delegation

#### Scenario: Missing referenced resource
- **WHEN** a create request names an identity, group, or zone binding that does not exist
- **THEN** DANS returns `404 Not Found` and creates no delegation

#### Scenario: Retired zone binding
- **WHEN** a create request names a retired zone binding
- **THEN** DANS returns `409 Conflict` and creates no effective authority

#### Scenario: Non-operator management attempt
- **WHEN** a non-operator attempts to create, edit, or revoke a delegation through an operator-management operation
- **THEN** DANS returns `403 Forbidden` and leaves delegation state unchanged

### Requirement: Exact and glob selector semantics
Every selector MUST be explicitly tagged as `exact` or `glob` and MUST contain a canonical absolute owner name or pattern within its zone binding. Exact selectors SHALL interpret every DNS name character literally. Glob selectors SHALL match the entire canonical absolute owner name, with `*` matching zero or more characters and `?` matching exactly one character across DNS label boundaries; all other characters SHALL be literal.

#### Scenario: Glob spans multiple labels
- **WHEN** an active delegation contains glob selector `*.apps.example.com.`
- **THEN** it can match both `one.apps.example.com.` and `one.two.apps.example.com.`

#### Scenario: Glob is anchored to the whole name
- **WHEN** an active delegation contains glob selector `api.*.example.com.`
- **THEN** it does not match `prefix.api.one.example.com.` or `api.one.example.com.invalid.`

#### Scenario: Exact selector is literal
- **WHEN** an active delegation contains exact selector `host.example.com.`
- **THEN** it matches only that canonical owner name and does not treat any character as a pattern operator

#### Scenario: Selector outside its zone
- **WHEN** an operator submits a relative selector or a selector outside the referenced zone binding
- **THEN** DANS returns `422 Unprocessable Entity` and creates no delegation

### Requirement: Apex and literal wildcard protection
The zone apex and an RRset whose owner name contains the DNS wildcard label `*` MUST be authorized by an exact selector. A glob selector MUST NOT authorize either resource, even if its pattern would otherwise match the canonical character sequence.

#### Scenario: Zone apex requires exact authority
- **WHEN** a non-operator has only glob selectors that could match names in `example.com.`
- **THEN** a change to the `example.com.` apex is forbidden

#### Scenario: Exact apex authority
- **WHEN** a non-operator has an active exact selector for `example.com.` and the other delegation restrictions match
- **THEN** the selector can authorize an apex RRset change

#### Scenario: Literal wildcard owner requires exact authority
- **WHEN** the requested owner name is the literal wildcard RRset `*.example.com.` and the caller has only a glob selector with the same character sequence
- **THEN** the change is forbidden

#### Scenario: Exact literal wildcard authority
- **WHEN** the requested owner name is `*.example.com.` and the caller has a matching active exact selector
- **THEN** the selector can authorize that literal wildcard RRset change

### Requirement: Whole-RRset authority with optional filters
A delegation SHALL authorize a requested RRset tuple only when one active grant matches the zone binding, complete owner-name selector, record type, and change kind together. A matching grant SHALL authorize the whole RRset, including its TTL, records, disabled flags, and comments; DANS MUST NOT assign ownership to individual record values. Overlapping delegations SHALL combine by union without deny rules or precedence.

#### Scenario: All restrictions match one grant
- **WHEN** one active delegation matches the requested owner name, RR type, and change kind
- **THEN** that delegation authorizes the complete RRset change

#### Scenario: Restrictions cannot be composed across grants
- **WHEN** no single active delegation matches the requested owner name, RR type, and change kind together
- **THEN** the change is forbidden even if separate delegations each match only some dimensions

#### Scenario: Individual-value operation still requires whole-RRset authority
- **WHEN** a caller uses `EXTEND` or `PRUNE` for one record value
- **THEN** DANS evaluates authority over the containing owner-name-and-type RRset rather than ownership of that value

#### Scenario: Overlapping grant remains effective
- **WHEN** two active delegations independently authorize the same RRset tuple
- **THEN** the tuple remains authorized while either delegation remains effective

### Requirement: Effective authority is the current union
DANS SHALL compute an identity's effective authority from its current direct delegations, the current delegations of its direct groups, and its operator role at one authorization decision point for each request. It MUST use current durable authority without a permission cache. An operator SHALL have authority over every compatible PowerDNS operation and RRset without requiring a delegation.

#### Scenario: Direct delegation
- **WHEN** an enabled identity has a direct active delegation matching an RRset tuple at the authorization decision point
- **THEN** the tuple is included in that identity's effective authority

#### Scenario: Group delegation
- **WHEN** an enabled identity is a current direct member of a group whose active delegation matches an RRset tuple
- **THEN** the tuple is included in that identity's effective authority

#### Scenario: Membership removed before decision
- **WHEN** group membership is removed before the authorization decision point
- **THEN** that group's delegations do not authorize the request

#### Scenario: Operator bypass
- **WHEN** an enabled operator submits a compatible zone `PATCH`
- **THEN** DANS does not require an RRset delegation for the requested changes

### Requirement: Whole-batch delegated authorization
For a non-operator zone `PATCH`, DANS MUST canonicalize a non-empty batch within the contract's declared maximum and authorize every requested RRset tuple before forwarding. Each tuple MAY be authorized by a different active delegation, but if any tuple is unauthorized DANS MUST reject the entire batch with `403 Forbidden`, MUST NOT split the batch, and MUST send no part of it to PowerDNS.

#### Scenario: Every batch member is authorized
- **WHEN** every RRset tuple in a valid batch is authorized by the caller's effective authority
- **THEN** DANS forwards exactly one batch containing all requested changes

#### Scenario: Mixed-authority batch
- **WHEN** at least one RRset tuple is authorized and at least one tuple is not authorized
- **THEN** DANS returns `403 Forbidden` and forwards none of the batch

#### Scenario: Different grants authorize different members
- **WHEN** every RRset tuple is authorized but different tuples match different active delegations
- **THEN** DANS forwards the complete batch as one request

#### Scenario: Empty delegated batch
- **WHEN** a non-operator submits a zone `PATCH` with no RRset change
- **THEN** DANS returns `422 Unprocessable Entity` and sends no upstream request

#### Scenario: Batch exceeds the declared maximum
- **WHEN** a zone `PATCH` contains more RRset changes than the combined API contract permits
- **THEN** DANS returns `422 Unprocessable Entity` and sends no upstream request

### Requirement: Permanent revocation and binding lifetime
DANS SHALL revoke a delegation irreversibly while retaining its immutable representation and revocation state. Repeating a revocation SHALL be harmless, revocation of one delegation MUST NOT revoke overlapping grants, and a delegation attached to a retired zone binding MUST never become effective again. A recreated zone MUST require a fresh binding and fresh delegations.

#### Scenario: Revocation precedes authorization
- **WHEN** a delegation is revoked before a request's authorization decision point and no other delegation authorizes the tuple
- **THEN** DANS rejects the requested change

#### Scenario: Request passed the decision point
- **WHEN** a request is authorized and its delegation is revoked after the authorization decision point
- **THEN** the in-flight request can finish, while the next request cannot use the revoked delegation

#### Scenario: Repeated revocation
- **WHEN** an operator revokes an already-revoked delegation
- **THEN** DANS reports the delegation as revoked without reactivating it or creating a second state transition

#### Scenario: One overlapping grant is revoked
- **WHEN** one of multiple overlapping delegations is revoked
- **THEN** another active matching delegation can continue to authorize the RRset tuple

#### Scenario: Zone binding is retired
- **WHEN** a delegation's zone binding is retired
- **THEN** that delegation is ineffective for all later authorization decisions and cannot be reactivated by recreating the upstream zone name
