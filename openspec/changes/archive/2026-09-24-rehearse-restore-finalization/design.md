## Context

See `proposal.md` for motivation and `specs/cli-configuration-qa/spec.md` for the required behavior. The integration harness already runs a disposable PostgreSQL/PowerDNS project for each supported PostgreSQL major and uses the built release image for offline maintenance. Restore finalization is implemented and has database-level tests, but no test restores a database backup before invoking it. The coordinated-upgrade guide correctly states that PostgreSQL does not identify a restored copy automatically.

## Goals / Non-Goals

**Goals:** Rehearse the exact offline finalization boundary against a restored database, verify credential invalidation through the public API, and keep evidence safe for CI publication.

**Non-Goals:** Detect arbitrary database clones, change production authentication or schema, automate an operator's disaster recovery, or back up PowerDNS state.

## Decisions

1. Extend the existing integration project rather than add a separate test stack. Take a PostgreSQL custom-format backup after the harness has created identities, delegated authority, and audit events; restore it into a second database in the same disposable PostgreSQL container. This reuses the existing image and release executable while keeping the source database untouched. A separate stack would duplicate setup and make the gate slower.
2. Add a profiled DANS service aimed at the restored database, with no startup until finalization succeeds. Run `restore finalize --confirm` through the release image with only the restored database URL, and stop the gate before starting that service if finalization fails. Starting a general API instance against the restored copy to test pre-finalization readiness would violate the supported procedure and imply clone detection that does not exist.
3. After finalization, start the restored service and use its public API to check readiness, operator identity, retained authorization/audit data, rejection of the captured old tokens, and acceptance of the single replacement token. Keep backup bytes, token values, and raw command output in disposable local storage; print only fixed check names and outcomes. The current CI integration wrapper remains the publication boundary.
4. Run this sequence in each existing PostgreSQL matrix leg. The restore rehearsal uses the same supported minor already selected by that leg; no new matrix or dependency is needed.

## Risks / Trade-offs

- A copied PostgreSQL database has no intrinsic restore marker → keep the supported operator order explicit in the runbook and do not claim the service can detect accidental early startup.
- A backup contains token digests and authorization history → keep it inside the disposable project, never upload it, and remove it with scoped teardown.
- Reusing the existing PowerDNS fixture does not rehearse a full upstream disaster restore → verify PostgreSQL authority and audit state only; document that PowerDNS backup/recovery remains a separate operator concern.
- A failed assertion could print a secret-bearing command response → capture raw output privately and emit only fixed failure labels through the existing CI wrapper.

## Migration Plan

No production migration is required. Land the integration gate and runbook together; rollback removes the rehearsal without altering data or production behavior.
