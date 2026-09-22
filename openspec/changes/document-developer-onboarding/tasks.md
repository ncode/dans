## 1. Match documentation to implemented behavior

- [x] 1.1 Confirm host probes, credential recovery, and lifecycle CI are implemented; reconcile target names and acceptance evidence with their linked proposals.
- [x] 1.2 Check prerequisites directly against Make targets, the integration harness, `go.mod`, generation configuration, and frontend/browser-test configuration; separate host tools from container tools.

## 2. Complete the onboarding path

- [x] 2.1 Add generic checkout/working-directory guidance and concise quickstart success indicators while retaining macOS/Linux and Docker Compose v2 plus Make as the evaluator path.
- [x] 2.2 Add a compact prerequisite/command table for evaluation, optional host probes, native Go work, frontend checks, and full integration; retain `make help` as the command reference.
- [x] 2.3 Add troubleshooting for Docker availability, occupied ports, readiness failures, and missing/rejected tokens, with status/log inspection before destructive remedies and explicit recovery limits.
- [x] 2.4 Explain stop/reset persistence, retained smoke fixtures, credential-output handling, and links to existing deployment/production recovery guidance; avoid duplicating runbooks.

## 3. Verify the documentation

- [x] 3.1 Check command spelling, repository-relative links, and rendered Markdown; run `make help` and the existing development contract without introducing tests that pin prose.
- [x] 3.2 Walk through evaluator and optional host-probe instructions on disposable supported environments or reuse matching-revision evidence; record exact coverage and any untested platform.
- [x] 3.3 Review all examples for private identifiers and secrets, confirm this change contains documentation only, and run strict OpenSpec validation with specs intentionally skipped.

## Verification notes

- `make help`, `./scripts/dev-contract.sh`, repository-relative link checks, shell syntax checks, and `git diff --check` passed on the current macOS checkout.
- Matching-revision disposable-stack evidence covers `make up`, `make smoke`, `make smoke-host`, stop/start persistence, token recovery, and default/overridden loopback ports on macOS. Linux lifecycle coverage is provided by the CI job; a native Linux run was not available locally.
- The implementation diff is documentation-only (`README.md` plus this checklist); no prose-pinning tests were added.
- No secrets or private machine identifiers are included. `openspec validate document-developer-onboarding --strict` passed with specs intentionally skipped.
