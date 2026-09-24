## 1. Transport regression

- [x] 1.1 Add a deterministic real `net/http` stalled-reader test that observes the production write deadline, an incomplete client-visible export, and no later page read.
- [x] 1.2 Add a loopback HTTP client-disconnect test that confirms request cancellation stops pending and subsequent page work.

## 2. Correction and verification

- [x] 2.1 If either test exposes a handler defect, fix the shared audit-export path without changing its public contract; otherwise confirm no production change is needed.
- [x] 2.2 Run the focused tests, full Go tests with race detection, and the local Docker integration suite; record concise, sanitized results.
- [x] 2.3 Review the implementation diff with OCR, resolve confirmed issues, and rerun affected checks.
- [x] 2.4 Open a PR through the authenticated account and verify its commit identities, privacy-safe body, and CI result.
