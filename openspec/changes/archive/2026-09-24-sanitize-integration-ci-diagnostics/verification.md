# Integration CI diagnostics verification

- `make integration-contract` passed. The failure, early-failure, invalid-phase, and success cases used synthetic confidential-looking values; only fixed summaries appeared in simulated CI output and artifact files.
- Strict OpenSpec validation, shell syntax checks, and `git diff --check` passed.
- `make integration` passed locally with disposable Docker services for PostgreSQL 16.14 and 18.4. Raw container output remains in private temporary storage and is not included here.
- OCR reviewed all seven eligible changed files and reported no findings. OpenSpec Markdown artifacts excluded by its filter were reviewed locally. A separate review found a CI failure-condition issue; the regression contract caught it before the fix, then passed after the fix.
- The macOS watchdog regression injected a missing process start-time lookup. With the prior watchdog line restored, the bounded launcher case failed because no escaped-process diagnostic was produced; removing that line made the same test pass through cleanup. The original CI timeouts had no phase output, so this proves the hang path but does not prove it caused those earlier runs.
- `make dev-contract` and `make integration-contract` passed after the watchdog change. OCR reviewed both eligible shell files in the expanded diff with no findings; excluded OpenSpec Markdown was reviewed locally.
- The pushed PR run passed unit, macOS shell, both PostgreSQL integration legs, and Linux development-stack lifecycle checks.
