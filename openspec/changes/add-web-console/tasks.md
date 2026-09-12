## 1. Baseline and public contracts

- [x] 1.1 Load the required OpenSpec, frontend design/accessibility/verification, Go, and TDD guidance; verify the isolated worktree baseline and record the existing verification commands.
- [x] 1.2 Define browser-session and indexed-browse OpenAPI operations, security/route classifications, schemas, and effective-self-delegation zone fields while preserving compatibility operations.
- [x] 1.3 Add forward-only browser-session and browse-index migrations with runtime-role grants and coordinated-upgrade/restore compatibility; generate database queries and API artifacts through existing tools.

## 2. Browser authentication

- [x] 2.1 Add failing behavior checks and implement opaque session creation, digest storage, original-token association, absolute seven-day expiry, bounded cleanup, and current revocation/disablement validation.
- [x] 2.2 Integrate header/session credentials into authentication and current-authority decision paths, including delegated-write SQL; reject ambiguous/duplicate credentials and preserve fail-closed behavior.
- [x] 2.3 Implement explicit sign-in/sign-out endpoints, protected cookie attributes, cross-origin protection, safe invalid-cookie clearing, and sign-out without original-token revocation.
- [x] 2.4 Verify session/token revocation, permission changes, expiry, CSRF, credential non-disclosure, and restore behavior across two instances while retaining header-token client tests.

## 3. Indexed browsing

- [x] 3.1 Implement canonical zone/name/type storage and full-RRset payload handling with staged generations and bounded streaming API ingestion; preserve the previous generation on any incomplete read.
- [x] 3.2 Implement 100-row keyset browsing, exact/prefix owner and type filters, deterministic ordering, and bounded filter/zone/generation-bound cursors with invalid/stale cursor checks.
- [x] 3.3 Implement shared bounded refresh claims, first-access collection, worker cancellation/recovery, atomic fenced publication, and no eager indexing during service startup.
- [x] 3.4 Expose browse/manual-refresh API responses with indexing, ready, refreshing, stale/error states and sanitized freshness metadata; preserve shared authenticated read visibility and readiness behavior.

## 4. Mutation freshness and lifecycle

- [x] 4.1 Persist affected-RRset read-reconciliation work through shared mutation intent/outcome paths for browser, CLI, and generated-client writes; re-read complete affected sets after REPLACE/DELETE/EXTEND/PRUNE.
- [x] 4.2 Reconcile unknown outcomes through reads only, handle zone-wide operations, and fence full/incremental publication so concurrent mutations cannot be lost when clearing pending work.
- [x] 4.3 Invalidate generations/work on supported deletion, retirement, and recreation; verify late workers cannot publish prior-lifetime content or revive authority.
- [x] 4.4 Schedule five-minute full refreshes for viewed zones, coalesce manual/periodic requests across instances, and verify interrupted/failed refreshes retain complete visibly stale data.

## 5. Frontend delivery and sign-in flow

- [x] 5.1 Add the React/TypeScript/Cloudscape static frontend, pinned lockfile, native same-origin API access, typecheck/component-test/build scripts, and task-focused console layout.
- [x] 5.2 Embed production assets and update clean local, Docker, CI, and release builds; verify public static routes cannot swallow API errors and no production Node process or external asset fetch is needed.
- [x] 5.3 Implement token sign-in, remembered-session recovery, sign-out, role-aware navigation, recoverable session failures, and protected-data clearing without browser credential storage.

## 6. DNS workflows

- [x] 6.1 Implement zone list/detail and operator zone create/edit/delete forms with live API outcomes and destructive confirmation.
- [x] 6.2 Implement the complete paginated RRset table with server filtering, Previous/Next state, first-index loading, full-refresh control, and ready/stale/error feedback.
- [x] 6.3 Implement complete live RRset create/edit/delete forms preserving multi-value/comment/disabled data and existing DNS semantics, including reverse zones, apex, wildcard, and long values.
- [x] 6.4 Implement pre-save/pre-delete live comparison, creation collision detection, preserved drafts and reconciliation, duplicate-submit prevention, and unknown-outcome handling without automatic retries.
- [x] 6.5 Implement advisory action availability and explanations from zone-enriched self-delegations; check complete-grant matching, type/action restrictions, apex/wildcard rules, and mid-edit revocation.

## 7. Delegation and audit workflows

- [x] 7.1 Implement operator delegation list/detail/create/revoke flows with existing identity/group and zone selection, exact/glob selectors, restrictions, pagination, and confirmation.
- [x] 7.2 Implement operator audit list/detail with useful filtering/pagination and distinct pending/success/failed/unknown states; keep non-operator access denied.

## 8. Verification and documentation

- [x] 8.1 Run targeted frontend tests, TypeScript checks, production builds, Go unit/race/vet checks, deterministic generation checks, and existing development/integration contracts; fix regressions.
- [x] 8.2 Exercise real supported PostgreSQL/PowerDNS integration with two application instances for auth, CRUD, grants, audit, refresh failures/races, and lifecycle behavior.
- [x] 8.3 Generate 400 synthetic zones including 250,000 RRsets; validate actual API-to-index ingestion, complete/filterable page traversal without whole-zone browser downloads, and the five-second cross-instance mutation visibility target with reproducible sanitized measurements.
- [x] 8.4 Measure canonical release artifact/OCI footprint and applicable idle/startup checks with the embedded UI; preserve existing budgets or surface an evidenced unmet requirement rather than silently changing it.
- [x] 8.5 Verify the rendered console's real sign-in/browse/edit/delegation/audit flows, keyboard/focus behavior, 200-percent zoom, narrow layouts, long values, and loading/empty/error/stale/permission states; document exact coverage and limitations.
- [x] 8.6 Update public API, CLI/development, build/release, and operations documentation plus the agreed design/ADRs where implementation clarifies details; run strict OpenSpec validation and review changed artifacts for privacy and scope.
