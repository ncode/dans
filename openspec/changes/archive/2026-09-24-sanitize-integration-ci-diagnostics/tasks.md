## 1. Reproduce the publication boundary

- [x] 1.1 Add a shell contract test for failing, early-failing, and successful CI integration runs using synthetic credentials, paths, and identifiers; confirm the failure cases expose the current unsafe behavior before implementing the wrapper.

## 2. Contain and summarize diagnostics

- [x] 2.1 Implement the CI-only integration entry point and fixed phase markers so raw harness output remains runner-local, failure status is preserved, and the published summary contains only allowlisted values.
- [x] 2.2 Change the workflow to invoke the entry point and upload only the summary directory on failure; strengthen the existing integration contract to reject raw-log artifact paths.

## 3. Verify and deliver

- [x] 3.1 Run focused shell contracts, strict OpenSpec validation, and local Docker integration for both supported PostgreSQL versions; record sanitized results.
- [x] 3.2 Review the exact diff with OCR, inspect files its filter excludes locally, fix confirmed findings, and rerun affected checks.
- [x] 3.3 Add a fast contract for fixed, nonsecret macOS test phase markers, observe it fail, then emit those markers at major test and wait boundaries.
- [x] 3.4 Reproduce the launcher-watchdog identity-miss deadlock with a focused failing regression, then keep the watchdog alive and bound the scenario wait while preserving existing assertions.
- [x] 3.5 Rerun affected local checks and OCR on the expanded diff, inspect excluded files locally, and update sanitized verification notes.
- [x] 3.6 Recheck outgoing privacy and commit identities, update the PR through the authenticated account, and confirm all CI checks pass.
