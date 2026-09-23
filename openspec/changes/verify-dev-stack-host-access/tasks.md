## 1. Host smoke entry point

- [x] 1.1 Add `make smoke-host` through the existing lifecycle script and document its optional `curl`/`dig` prerequisites in `make help` and a short README reference.
- [x] 1.2 Check host prerequisites before fixture creation, reuse one existing smoke invocation, and preserve ordinary `make smoke` behavior and its Docker-only prerequisites.

## 2. Published interface probes

- [x] 2.1 Add bounded host HTTP probes for the expected readiness response and console shell using the effective loopback HTTP port and bypassing proxies.
- [x] 2.2 Add separate nonrecursive host UDP and TCP DNS probes using the effective DNS port; validate status, authority, exact owner/type/value, and disable UDP-to-TCP fallback.
- [x] 2.3 Emit concise protocol/port diagnostics on failure without printing credentials or altering listener exposure.

## 3. Verification

- [x] 3.1 Add focused runnable checks for missing tools before mutation, overridden ports, proxy bypass, wrong HTTP/DNS content, bounded failures, and strict separation of UDP/TCP results.
- [x] 3.2 Verify default and overridden ports on the disposable macOS Docker stack; Linux runtime was unavailable in this workspace and remains covered by CI rather than being claimed as passed.
- [x] 3.3 In disposable verification, independently break HTTP, UDP DNS, and TCP DNS publication while internal smoke stays healthy, and confirm host smoke detects each failure.
- [x] 3.4 Run the development contract and applicable shell checks; validate this OpenSpec change strictly and review sanitized evidence before publication.
