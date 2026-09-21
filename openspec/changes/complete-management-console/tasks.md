## 1. Contract and backend reads

- [x] 1.1 Add literal handle-prefix filtering to identity/group and relevant membership collections; test punctuation, validation, visibility, and filter/parent-bound cursors.
- [x] 1.2 Add operator identity membership, effective-delegation, and retained-assignment reads; test disabled targets/groups, provenance, retired lifetimes, and non-operator rejection.
- [x] 1.3 Add self-service current-token metadata and binding recovery eligibility; test secret exclusion, session backing-token identity, and stale eligibility enforcement.
- [x] 1.4 Update OpenAPI/access classes and generated Go/SQL artifacts for all new reads, preserving old clients and deterministic generation.

## 2. Streamed audit export

- [x] 2.1 Add filtered NDJSON HTTP download using bounded pages and current credential/role checks before each page; preserve existing CLI export.
- [x] 2.2 Implement route-scoped deadline handling, cancellation, and transport abortion after headers; test expiry/revocation/demotion, read/write failure, no hidden row cap, and no false successful truncation.
- [x] 2.3 Extend audit filters and native browser download UI; verify completed and interrupted downloads without a full frontend Blob or a snapshot claim.

## 3. Management console workflows

- [x] 3.1 Add bounded shared selectors and paginated management controls, removing whole-collection identity/group selector loads and avoiding incomplete-grant false denials.
- [x] 3.2 Implement operator identity list/create/details/update, groups/effective/retained authority views, kind filtering, and current-state/restore explanations.
- [x] 3.3 Implement group list/create/details/update and direct membership add/remove, with pagination and prefix search.
- [x] 3.4 Implement self-service profile/groups/authority and self/operator token list/create/revoke; keep secrets one-time with Copy and mark the sign-in token.
- [x] 3.5 Implement confirmation and session/role transitions for self-revocation, disable/re-enable, and self-demotion, preserving last-enabled-operator protection and no automatic mutation retries.
- [x] 3.6 Implement paginated binding list/details/create, observation, and eligible confirm-absent/retry-delete/rebind workflows with explicit consequences and unknown outcomes.

## 4. Verification and documentation

- [x] 4.1 Run focused backend integration against real PostgreSQL for relationships, search, authority changes across instances, and streamed export behavior.
- [x] 4.2 Run frontend type/unit/build checks and rendered operator/self-service workflows, including keyboard/focus, labels, narrow layouts, 200 percent zoom, errors, and Safari-compatible token/download behavior.
- [x] 4.3 Validate the agreed synthetic management dataset and record bounded pagination, selector search beyond the first page, complete audit traversal, and memory evidence.
- [x] 4.4 Run Go race/vet, generated-contract checks, existing integration/browser regressions, development contracts, and applicable release/resource gates; investigate failures introduced by this change.
- [x] 4.5 Update API/CLI/frontend documentation and record sanitized verification evidence, exact checks, and remaining limitations without claiming unperformed validation.

## 5. Review and corrections

- [x] 5.1 Review the exact implementation diff with agreed requirements as business context, excluding unrelated tooling and private evidence; verify review coverage and successful execution.
- [x] 5.2 Investigate every finding, fix confirmed issues, and rerun affected tests and review as warranted; record dispositions for rejected findings and leave no confirmed unresolved findings.
- [x] 5.3 Validate the complete OpenSpec change and final diff, ensuring every specified behavior and required verification is complete before marking all tasks done.
