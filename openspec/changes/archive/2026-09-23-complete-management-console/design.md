## Context

See `proposal.md` for motivation and `docs/frontend.md` for the agreed interview decisions. The implemented first console release supplies the React/TypeScript/Cloudscape shell, same-origin request helper, CSRF-protected token-backed sessions, and cursor controls. Existing management APIs cover mutations; missing relationships and search conveniences should be additive reads rather than new authority models.

Current membership reads include disabled resources, while effective-delegation reads assume an enabled authenticated self. Existing audit NDJSON helpers page through database results but reuse an actor snapshot. Binding responses do not expose all state used to decide reconciliation eligibility. These seams need explicit treatment, not browser-side reconstruction of security state.

## Goals / Non-Goals

**Goals:** Complete the specified workflows with existing components, generated contracts, bounded reads, and current request-time authorization. Keep secrets transient and lifecycle consequences visible.

**Non-Goals:** Replace the frontend stack, add a generic administration framework, change authorization semantics, implement token scopes or credential recovery, or introduce background export infrastructure. Preserve the proposal's other exclusions.

## Decisions

### Reuse the console and keep collections bounded

Add Identities, Groups, and Zone bindings to operator navigation and a self-service entry available to every signed-in identity. Use existing tables, explicit forms, confirmation dialogs, loading/error states, and Previous/Next controls. Keep users and service identities in one identity collection with a kind filter. Use a shared bounded identity/group selector only where actual membership/delegation forms require it; do not add a general-purpose form framework.

Add `handle_prefix` to relevant identity/group and membership reads, preserve exact `handle`, and bind normalized filters into existing cursors. Match punctuation literally, reject invalid prefixes, and restart paging after filter changes. Preserve the existing HTTP 422 response for invalid collection cursors rather than changing client-visible error behavior. Avoid client-wide `allPages` accumulation. Permission explanations must distinguish incomplete advisory data from a confirmed absence of authority; a loaded first page must never become a false denial or override server authorization.

### Add narrow relationship and credential reads

Provide operator reads under the target identity for groups, effective delegations, and retained delegation assignments. Retained assignments expose direct/group provenance and applicable resource states; suspended assignments remain inspectable, but revoked grants and retired lifetimes never become effective again. Return no effective delegations for a disabled target. Keep the existing operator role explicit, outside the delegation list. Reuse underlying read/query logic while separately enforcing caller authorization and target enabled state.

Expose the authenticated caller's backing token ID as additive self-service metadata, derived from the existing authenticated actor. Preserve the current identity representation and old clients. Do not expose session identifiers, digests, or secrets. Token creation remains an explicit existing mutation, with the returned secret held only in the one-time view. Revocation and self-disable/demotion confirmations describe consequences; existing server checks remain authoritative.

### Expose binding recovery eligibility from the existing state

Extend operator binding reads with the minimum advisory lifecycle/eligibility metadata derived from existing reconciliation state. Do not reconstruct eligibility by scanning audits or introduce another state machine. Separate read-only upstream observation from confirm-absent, retry-delete, and rebind submissions. Refresh after mutations and retain unknown outcomes without automatic replay. Mutations continue to enforce their existing state and authorization checks.

### Stream audit downloads through HTTP

Add an operator-only `GET /api/v1/dans/audit-events/export` attachment endpoint using existing exact filters and NDJSON encoding. Page through history in existing order, retain only a page at a time, and reauthenticate the actual request credential and operator role before each page. This includes session validity as well as backing token/identity validity; an actor captured at request start is insufficient.

Use ordinary browser download handling rather than accumulating a full Blob in frontend memory. The UI reports download initiation, not successful completion. Validate inputs and initial authorization before headers; after streaming begins, a read/authentication/write failure aborts the transport instead of appending JSON errors or normally ending a partial file. Cancellation ends further page work. Scope any necessary timeout/deadline adjustment to this route and retain bounded stalled-write behavior; do not disable timeouts for normal API requests. Verify actual download failure behavior in supported browsers.

Each page reads live history. Concurrent commits can be included or missed according to their order and visibility; no database-wide snapshot, background job, total count, or hidden total-row cap is introduced.

### Keep integration and review evidence scoped

Extend the existing generated OpenAPI/SQL flow and existing test harnesses. Cover new authorization and streaming paths with meaningful regression tests, and verify synthetic management capacity separately from the existing DNS capacity benchmark. Follow the existing frontend accessibility, verification, and Go conventions.

After implementation, review the exact change, inspect its coverage and every reported finding, fix confirmed issues, and rerun the affected checks. Keep raw review output and operational evidence private; only sanitized summaries belong in project documentation. Do not include unrelated untracked tooling in review scope.

## Risks / Trade-offs

- Disabled identities/groups retain access that can return → separate effective authority and retained assignments, and explain restoration before confirmation.
- Current-token revocation or self-disablement ends access → mark the token and confirm consequences; preserve the existing operator invariant without inventing a last-token guarantee.
- Large relation collections can make apparently small forms unbounded → server-side prefix search and pagination, including selectors; never interpret partial grants as complete policy.
- Browser/infrastructure timeouts can truncate large exports → bounded streaming, route-specific timeout handling, transport failure after headers, and real completion/interruption checks.
- Authority may change during a download → reauthenticate each page, preserving the existing authorization-decision-point semantics for a page already authorized.
- Additive UI/API contracts can drift → update generated contracts and documentation together and test browser workflows against the real API.

## Migration Plan

Build on the existing console commit. No new identity, session, or lifecycle storage model is required; reuse existing tables and introduce an index migration only if measured query behavior requires it. Generate artifacts with the pinned toolchain, rebuild embedded assets, and retain coordinated same-version deployment and runtime grant rules. Verify old header-token clients and local HTTP browser sign-in. Rollback follows existing schema rules if a measured index addition requires migration; do not promise in-place schema downgrade.
