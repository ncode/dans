## Why

The embedded console supports DNS editing and delegations, but managing identities, groups, credentials, and zone-lifetime recovery still requires the CLI. Complete those workflows and improve access troubleshooting and audit export using the agreed design in `docs/frontend.md`.

## What Changes

- Add operator identity and group administration, direct membership management, and self-service profile, membership, authority, and token views in the existing React/TypeScript/Cloudscape console.
- Distinguish current effective authority from retained assignments, including disabled identities and groups; expose narrowly scoped operator relationship reads.
- Show newly created token secrets once, identify the token backing the current sign-in, and confirm changes that affect the caller's own access while preserving last-enabled-operator protection.
- Add literal handle-prefix search and bounded pagination to management lists and identity/group selectors, retaining exact-handle API lookup.
- Add state-guided zone binding inspection, observation, and explicit recovery actions without restoring retired delegations.
- Expose all existing audit filters and stream all matching events as NDJSON with bounded memory, current-authority rechecks, and visible interrupted-download failure. Export traverses live history rather than a snapshot.
- Verify synthetic capacity of 1,000 identities, 100 groups, 1,000 members in a group, 100 tokens per identity, and 100,000 audit events; retain the existing browser, DNS, and release checks.

## Capabilities

### New Capabilities

- `management-console`: Accessible operator and self-service management workflows, recovery presentation, token handling, and bounded browsing/downloads.

### Modified Capabilities

- `identity-access-management`: Operator relationship and retained-assignment reads, current-credential metadata, and literal handle-prefix collection filtering.
- `audit-zone-lifecycle`: Authenticated streaming NDJSON audit export with request-time authority checks and explicit incomplete-transfer semantics.

## Impact

Builds on the implemented `add-web-console` change. Affects frontend navigation/forms, management handlers and database reads, the OpenAPI overlay/generated clients, collection queries, HTTP download handling, and focused integration/browser tests. Additive APIs preserve existing CLI clients, authentication, role/lifecycle rules, and the single executable. Reuse installed dependencies and existing pagination/NDJSON facilities. Independent code review and resolution of confirmed findings are required before completion.

## Non-goals

Advanced PowerDNS administration, IdP integration, new roles or token scopes, hard deletion of identities/groups, token-secret recovery, CSV/date-range audit filters, snapshot exports, background export jobs, and automatic mutation replay are outside this change.
