## ADDED Requirements

### Requirement: Binding recovery state for management clients
DANS SHALL expose the current binding state and advisory recovery eligibility needed by operator clients without requiring them to reconstruct lifecycle state from audit history. Eligibility SHALL derive from the existing reconciliation rules, confer no authority, and be rechecked when an action is submitted.

#### Scenario: Inspect reconciliation eligibility
- **WHEN** an operator inspects a binding whose deletion is pending, failed, unknown, completed, or confirmed absent
- **THEN** the response distinguishes the state needed to present eligible recovery actions
- **AND** stale eligibility never bypasses the server's current mutation checks

### Requirement: Streaming HTTP audit export
DANS SHALL provide operator-only HTTP export of all audit events matching the existing actor, action, target type, target ID, and result filters as NDJSON, with one complete sanitized event per line. Export SHALL traverse the existing descending timestamp and resource-ID order with bounded memory and no implicit total-row cap. It SHALL NOT promise a cross-page snapshot or mutate audit history. Existing paginated reads and CLI export SHALL remain compatible.

#### Scenario: Export multiple pages
- **WHEN** an authorized operator requests matching history spanning multiple pages
- **THEN** the HTTP download emits all traversed matching events without buffering the complete export
- **AND** it identifies its NDJSON media type and uses a non-secret download filename

#### Scenario: Revoke access during export
- **WHEN** the caller's token, browser session, identity, or operator authority becomes invalid before a later page authorization decision
- **THEN** DANS stops before reading or emitting that later page
- **AND** an already-started transfer fails rather than ending as a successful complete export

#### Scenario: Interrupt an export
- **WHEN** the client cancels, the database fails, or a stream deadline is exceeded before completion
- **THEN** DANS promptly stops further page work and an already-started transfer is observably incomplete
- **AND** DANS does not append an error object that could be mistaken for a valid audit event or silently truncate with successful completion

#### Scenario: Traverse concurrent history
- **WHEN** audit events are committed while an export is traversing pages
- **THEN** each page reflects its own current authorized read view
- **AND** the API does not claim an atomic snapshot or guarantee inclusion of concurrent commits
