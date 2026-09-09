## Purpose

Define durable security evidence and zone-lifetime behavior so DNS mutations remain attributable and deleted authority cannot silently return.

## Requirements

### Requirement: Append-only security audit history
DANS SHALL retain an immutable audit history for security-sensitive management changes and authenticated authorization denials. Audit entries MUST identify the event, time, request correlation, actor when known, action, target, decision or result, and sanitized details without containing API-token secrets, PowerDNS credentials, TSIG secrets, DNSSEC private material, or other secret request fields.

#### Scenario: Record a management change
- **WHEN** DANS commits an identity, group, membership, operator-role, token, delegation, bootstrap, recovery, restore-finalization, or zone-lifecycle change
- **THEN** the same externally observed change has an immutable audit entry identifying its actor, action, target, and result

#### Scenario: Deny an authenticated operation
- **WHEN** an authenticated identity is denied a requested operation by current authorization state
- **THEN** DANS leaves the protected resource unchanged
- **AND** appends a sanitized denial entry identifying the actor, requested action, target, and denial result

#### Scenario: Protect audit secrets
- **WHEN** an operator reads or exports audit history for token issuance or a secret-bearing PowerDNS operation
- **THEN** the audit data contains identifiers and sanitized metadata
- **AND** contains no raw DANS token, upstream API key, TSIG secret, or DNSSEC private material

### Requirement: Audit-gated management changes
DANS MUST persist the audit entry for a management state change as part of the same externally atomic operation. If that audit entry cannot be retained, DANS SHALL reject the management change and leave its prior state intact.

#### Scenario: Audit storage rejects a management change
- **WHEN** an authorized management mutation cannot retain its required audit entry
- **THEN** DANS responds with a service-unavailable error
- **AND** the requested management state does not change

#### Scenario: Denial auditing is unavailable
- **WHEN** DANS denies an authenticated request but cannot retain the denial entry
- **THEN** the request remains denied
- **AND** DANS reports unhealthy until durable audit recording recovers

### Requirement: Durable DNS mutation intent
DANS MUST append an immutable audit intent before forwarding any DNS mutation to PowerDNS. The intent SHALL identify the caller, request correlation, classified operation, affected zone or zone binding, and sanitized mutation; inability to retain the intent MUST prevent the upstream request.

#### Scenario: Persist intent before forwarding
- **WHEN** a DNS mutation passes authentication and authorization
- **THEN** DANS durably records its audit intent before sending the mutation to PowerDNS
- **AND** the recorded intent can be correlated with the eventual outcome

#### Scenario: Fail before forwarding without intent
- **WHEN** DANS cannot retain the required audit intent
- **THEN** DANS responds with a service-unavailable error
- **AND** does not send the mutation to PowerDNS

#### Scenario: Reject a mutation before intent
- **WHEN** a DNS mutation is denied by authentication or authorization
- **THEN** DANS does not create a forwardable mutation intent
- **AND** an authenticated authorization denial is recorded under the denial rules

### Requirement: Terminal DNS mutation outcome
DANS SHALL append one immutable terminal outcome for each forwarded mutation intent: `succeeded`, `failed`, or `unknown`. DANS MUST preserve the response actually observed from PowerDNS even if outcome persistence fails, MUST NOT automatically replay the mutation, and MUST resolve any intent still pending past the upstream deadline to `unknown`.

#### Scenario: Record an observed PowerDNS result
- **WHEN** PowerDNS returns a definite success or failure for a forwarded mutation
- **THEN** DANS records the corresponding terminal outcome linked to the intent
- **AND** returns the observed PowerDNS response to the caller

#### Scenario: Fail to persist an observed outcome
- **WHEN** PowerDNS responds but DANS cannot retain the terminal outcome
- **THEN** DANS still returns the observed PowerDNS response without rewriting it
- **AND** does not replay the mutation
- **AND** reports unhealthy until audit storage recovers

#### Scenario: Reach an unknown outcome
- **WHEN** a forwarded mutation has no durable terminal outcome by the upstream deadline
- **THEN** DANS classifies and durably records its terminal outcome as `unknown`
- **AND** never replays it automatically

### Requirement: Operator audit access and retention
DANS SHALL expose audit history only to operators through a cursor-paginated read API and an NDJSON CLI export. Audit history MUST remain immutable and retained indefinitely in version 1, with no API or CLI operation that edits, deletes, or prunes entries.

#### Scenario: Read paginated audit history
- **WHEN** an operator requests audit history with supported exact filters and a valid cursor
- **THEN** DANS returns visible entries in stable descending timestamp and resource-ID order with a continuation cursor when more entries exist

#### Scenario: Export audit history
- **WHEN** an operator requests an NDJSON audit export
- **THEN** DANS emits one complete sanitized audit entry per line
- **AND** does not mutate or mark exported entries

#### Scenario: Deny non-operator audit access
- **WHEN** a non-operator requests audit history or an export
- **THEN** DANS responds with HTTP 403
- **AND** returns no audit entries

#### Scenario: Attempt audit deletion
- **WHEN** any caller attempts to edit, delete, or prune audit history
- **THEN** DANS rejects the operation
- **AND** every retained entry remains unchanged

### Requirement: Zone bindings identify zone lifetimes
DANS SHALL assign an immutable active zone binding to each zone lifetime that it manages, and delegations SHALL reference that binding rather than only the zone name. Retired bindings MUST never become active again, and a newly bound or rebound zone MUST receive a new resource ID without copying delegations from an earlier binding.

#### Scenario: Bind a managed zone
- **WHEN** DANS adopts a current upstream zone lifetime for management
- **THEN** DANS exposes one active zone binding with a unique resource ID and canonical zone name

#### Scenario: Recreate a zone name
- **WHEN** a zone name previously associated with a retired binding exists again upstream
- **THEN** DANS applies no delegation from the retired binding
- **AND** requires an explicit operator rebind before delegated management can resume

#### Scenario: Rebind an existing zone
- **WHEN** an operator explicitly rebinds a present zone whose previous binding is retired
- **THEN** DANS creates a new active binding with a new resource ID
- **AND** the new binding begins with no delegations

### Requirement: Revoke authority before zone deletion
DANS MUST retire the active zone binding and revoke every delegation attached to it before forwarding a zone-deletion request to PowerDNS. Failure to retire the binding, revoke its delegations, or retain the mutation intent MUST prevent the upstream deletion request.

#### Scenario: Delete a bound zone
- **WHEN** an operator requests deletion of a zone with an active binding
- **THEN** DANS retires that binding and revokes all attached delegations before forwarding the delete to PowerDNS
- **AND** delegated requests evaluated afterward receive no authority from that binding

#### Scenario: Fail before upstream deletion
- **WHEN** DANS cannot durably retire the binding, revoke its delegations, or retain the deletion intent
- **THEN** DANS does not send the zone deletion to PowerDNS
- **AND** reports the failure to the operator

### Requirement: Preserve retirement across deletion outcomes
DANS SHALL treat upstream deletion success or confirmed absence as completed deletion. A definite failure or unknown outcome MUST leave the binding retired and its delegations revoked; DANS MUST NOT automatically restore authority, retry deletion, or infer that a same-named zone is the previous lifetime.

#### Scenario: PowerDNS deletes the zone
- **WHEN** PowerDNS reports successful deletion or that the zone is already absent
- **THEN** DANS records deletion completion against the retired binding
- **AND** the binding and revoked delegations remain historical and inactive

#### Scenario: PowerDNS rejects deletion
- **WHEN** PowerDNS returns a definite deletion failure
- **THEN** DANS returns the observed failure
- **AND** leaves the binding retired and all attached delegations revoked pending operator reconciliation

#### Scenario: Record deletion status before consuming the response body
- **WHEN** an initial deletion or explicit deletion retry receives an upstream HTTP response
- **THEN** a 2xx status or 404 determines successful completion and any other status determines a definite failure
- **AND** the audit outcome write completes or reports failure before consuming or closing the response body
- **AND** a slow, truncated, or unread response body does not change that outcome to `unknown`
- **AND** the audit intent retains its request digest while the deletion outcome omits the optional response-body digest

#### Scenario: Zone deletion has unknown outcome
- **WHEN** DANS cannot determine whether PowerDNS applied a forwarded zone deletion
- **THEN** DANS marks the outcome `unknown`
- **AND** leaves the binding retired and all attached delegations revoked
- **AND** does not retry or restore authority automatically

### Requirement: Explicit zone-deletion reconciliation
DANS SHALL require an operator to reconcile every failed or unknown zone deletion explicitly. Reconciliation MUST preserve the retired binding and old delegation revocations while allowing the operator to confirm absence, retry deletion as a new audited mutation, or bind a currently present zone as a new lifetime with no inherited authority.

#### Scenario: Confirm that the zone is absent
- **WHEN** an operator reconciles a retired binding after verifying that its upstream zone is absent
- **THEN** DANS marks deletion complete for that binding
- **AND** retains the binding and its delegations as inactive history

#### Scenario: Retry deletion explicitly
- **WHEN** an operator chooses to retry deletion for a zone that is still present
- **THEN** DANS creates a new audit intent and forwards one new delete request
- **AND** keeps the old binding retired throughout the attempt

#### Scenario: Keep or recreate the zone explicitly
- **WHEN** an operator chooses to manage a currently present same-named zone after a retired deletion attempt
- **THEN** DANS creates a new active zone binding with a new resource ID
- **AND** copies no delegation from the retired binding

### Requirement: Unsupported out-of-band zone lifecycle changes
DANS MUST fail closed rather than reuse authority when a zone is created, deleted, or recreated outside the DANS control-plane API. DANS SHALL audit only mutations that pass through it and SHALL NOT claim audit coverage for direct PowerDNS API, backend, or local administration changes.

#### Scenario: Detect an out-of-band recreation
- **WHEN** DANS observes a same-named upstream zone whose prior binding is retired and no explicit rebind exists
- **THEN** DANS grants no delegated write authority for that zone
- **AND** requires operator reconciliation or rebind

#### Scenario: Mutate PowerDNS outside DANS
- **WHEN** an administrator changes zone lifecycle directly through PowerDNS or its backend
- **THEN** DANS does not represent that change as a DANS-audited mutation
- **AND** does not attach old delegations to the resulting zone
