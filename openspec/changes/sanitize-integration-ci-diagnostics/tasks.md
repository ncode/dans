## 1. Reproduce the publication boundary

- [x] 1.1 Add a shell contract test for failing, early-failing, and successful CI integration runs using synthetic credentials, paths, and identifiers; confirm the failure cases expose the current unsafe behavior before implementing the wrapper.

## 2. Contain and summarize diagnostics

- [x] 2.1 Implement the CI-only integration entry point and fixed phase markers so raw harness output remains runner-local, failure status is preserved, and the published summary contains only allowlisted values.
- [x] 2.2 Change the workflow to invoke the entry point and upload only the summary directory on failure; strengthen the existing integration contract to reject raw-log artifact paths.

## 3. Verify and deliver

- [x] 3.1 Run focused shell contracts, strict OpenSpec validation, and local Docker integration for both supported PostgreSQL versions; record sanitized results.
- [x] 3.2 Review the exact diff with OCR, inspect files its filter excludes locally, fix confirmed findings, and rerun affected checks.
- [ ] 3.3 Verify outgoing privacy and commit identities, open a PR through the authenticated account, update its body, and confirm all CI checks pass.
