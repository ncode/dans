## Purpose

Provide a usable, accessible DNS administration console for delegated users and privileged operators while preserving the existing API's authorization and mutation behavior.

## ADDED Requirements

### Requirement: Embedded console delivery
The released executable SHALL serve the console and its required assets alongside the API under the same origin without a separate runtime JavaScript server or third-party asset fetches. Static routes SHALL serve no protected DNS or identity data and MUST NOT turn undeclared API or upstream routes into HTML responses. Existing health, API documentation, and CLI workflows MUST continue to work.

#### Scenario: Open the released console
- **WHEN** a user opens the console from a canonical release installation
- **THEN** the sign-in interface and required local assets load without a separate frontend process

#### Scenario: Unknown API route
- **WHEN** a request targets an undeclared API route
- **THEN** the existing API rejection behavior remains in effect instead of receiving the application shell

### Requirement: Zone and RRset workflows
The console SHALL provide zone listing, zone detail, and zone creation, editing, and deletion for authorized operators. Within a zone it SHALL provide the agreed indexed RRset table and creation, editing, and deletion of complete RRsets subject to current delegated or operator authority. Forms SHALL expose owner name, record type, TTL, and record values, retain relevant existing record/comment state, and use explicit Save. Reverse zones, apex owners, literal wildcard owners, and supported record types MUST retain their existing DNS semantics.

#### Scenario: Operator manages a zone
- **WHEN** an operator creates, edits, or confirms deletion of a zone through the console
- **THEN** the console uses the public API and reports the observed outcome under the existing lifecycle contract

#### Scenario: Delegated user edits an RRset
- **WHEN** a user edits an RRset covered by current authority
- **THEN** the complete live RRset is loaded and the submitted change follows the public API's validation, authorization, and audit path
- **AND** merely opening or changing a form does not mutate DNS

#### Scenario: Long or multiple values
- **WHEN** an RRset has long text or multiple values
- **THEN** its values remain inspectable and editable without silently truncating, dropping, or merging them

### Requirement: Delegation and audit administration
Operators SHALL be able to list, inspect, create, and revoke delegations and view paginated audit intents and outcomes through the console. Grantee and zone selection SHALL use existing resource IDs with human-readable labels, and creation SHALL support exact/glob selectors and optional type/change restrictions without changing their semantics. Identity/group administration UI is excluded from this release; existing resources SHALL still be selectable. Non-operators MUST NOT gain administrative data or actions through navigation or direct API calls.

#### Scenario: Create and revoke delegated authority
- **WHEN** an operator selects an existing grantee and zone, supplies valid selectors and restrictions, and confirms the operation
- **THEN** the console creates or revokes the delegation through the existing public API

#### Scenario: Examine an uncertain mutation
- **WHEN** an operator opens audit history for an unknown outcome
- **THEN** the view distinguishes it from confirmed success or failure and does not offer automatic replay

### Requirement: Advisory permission explanations
The console SHALL explain which RRset actions appear available using the caller's current role and effective delegations with zone information. Explanations MUST preserve whole-RRset grants, exact apex/literal-wildcard protection, complete per-grant matching, and action/type restrictions. They MUST NOT claim to grant authority, hide ordinary DNS data as though writes controlled visibility, or bypass server rejection after a policy change.

#### Scenario: Permission changes while editing
- **WHEN** an operator revokes a relevant grant after the page was rendered
- **THEN** a later denied write is explained without discarding the draft or treating the earlier enabled control as authority

### Requirement: Detect stale edits without claiming atomic writes
The console SHALL obtain a complete live RRset when opening an editor and re-read it before saving or deleting an existing RRset. If the live value differs from the editor's baseline, it SHALL retain the draft, display the difference, and require reconciliation before another submission. Creation MUST NOT silently replace an RRset that appeared since the form was opened. This check MUST NOT be described as eliminating the existing check/write race.

#### Scenario: Another client changes the RRset
- **WHEN** the live RRset changes after an editor was opened but before its pre-save read
- **THEN** the draft is preserved, the conflict is shown, and no mutation is submitted until the user reconciles it

#### Scenario: Live read fails
- **WHEN** the authoritative read required for editing or pre-save validation fails
- **THEN** the console retains the draft and does not submit using indexed data as a substitute

### Requirement: Honest outcomes and destructive confirmation
Destructive actions SHALL require explicit confirmation. The console SHALL distinguish confirmed success, confirmed failure, and uncertain outcomes; it MUST NOT automatically retry mutations after timeouts, connection errors, or ambiguous responses. Refresh failures and partial background work MUST NOT display success or an empty zone falsely.

#### Scenario: Cancel deletion
- **WHEN** the user cancels a destructive confirmation
- **THEN** no mutation is sent and focus returns to a useful control

#### Scenario: Connection is lost after submission
- **WHEN** the browser cannot establish whether a submitted mutation completed
- **THEN** it reports the uncertainty, preserves useful context, and does not resubmit automatically

### Requirement: Accessible dense-console states
The console SHALL provide labeled controls, keyboard navigation, visible focus, accessible dialogs, and understandable loading, empty, error, stale, success, selected, and permission-limited states. It SHALL remain usable at 200 percent zoom and narrow widths, allowing local table scrolling where needed. Status MUST NOT depend only on color, and entered data MUST survive recoverable errors.

#### Scenario: Keyboard-only editing
- **WHEN** a user navigates from a zone table into editing and a destructive confirmation using only the keyboard
- **THEN** controls are named, focus remains visible and logical, dialogs can be dismissed, and closing restores useful focus

#### Scenario: Narrow layout and long content
- **WHEN** the console displays long DNS values at a narrow width or 200 percent zoom
- **THEN** navigation and actions remain reachable and content is not silently clipped
