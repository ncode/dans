## Context

See [proposal.md](proposal.md). The README already has an architecture diagram, a console introduction, and production links. Missing information belongs near the relevant commands rather than in another documentation site. Host probes and credential recovery are separate changes whose final behavior must be verified before being described as available.

## Goals / Non-Goals

**Goals:** Let evaluators and contributors find the prerequisites, expected result, and next diagnostic step without inspecting implementation scripts.

**Non-Goals:** Change command behavior, prescribe a single Docker provider, introduce runtime requirements, or duplicate the existing operations runbooks. Specs are intentionally skipped because this change only documents behavior.

## Decisions

### Keep one short entry point

Use README sections for checkout/working directory, quickstart, optional host verification, contributor checks, and troubleshooting. Give generic acquisition instructions or a repository URL only if its publication is already authorized; never embed a local checkout path. Keep `make help` authoritative rather than copying every target description.

Use a compact prerequisite table tied to actual commands: Docker Compose v2 and Make for evaluation; host `curl`/`dig` additionally for `smoke-host`; the toolchain in `go.mod` for native Go work; the pinned generation toolchain for generated artifacts; Node/npm and the Playwright browser for frontend checks; Docker, `curl`, `dig`, `jq`, and standard shell tools for full integration. Explain which tools are host requirements and which run inside Docker. Verify versions against repository configuration at implementation time.

### Troubleshoot from observation to a scoped next action

Cover Docker daemon/Compose availability, occupied ports and both overrides, readiness failures, missing or rejected tokens, and stop/reset differences. Start with status and logs. Describe automatic recovery only for the implemented local conditions; disabled/demoted operators and dependency failures require investigation. Do not present destructive reset as the default repair.

Explain that `down` preserves database/DNS/credential state, reset requires `CONFIRM=1`, smoke retains synthetic fixtures, and startup's sign-in output contains a secret that must not be pasted into issues or logs. Link to existing production recovery, backup, and deployment guidance instead of transferring local automatic recovery promises to production.

### Validate instructions as a walkthrough

Check links and target names, then follow the applicable evaluator/host-probe steps on disposable macOS and Linux environments, using the preceding lifecycle evidence where it already covers the same revision. Verify contributor prerequisites against the scripts and frontend configuration. Record only sanitized results and state any platform not exercised. Documentation-only edits do not justify a new suite of tests that grep for exact prose.

## Risks / Trade-offs

- Tool versions and command behavior drift → Reference the existing source of truth and keep examples minimal.
- Documentation gets ahead of code → Land the linked behavior changes first and verify every described target.
- Troubleshooting output exposes local credentials or identifiers → Use placeholders and concise sanitized examples; never publish captures or raw startup output.

## Migration Plan

Land after the other three changes, then review the rendered README and existing links. Rollback is limited to documentation; no deployment or data migration is involved.
