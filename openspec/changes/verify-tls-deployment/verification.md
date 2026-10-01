# Verification

The upgrade rehearsal was archived after its merged PR passed all hosted CI checks. Its complete added requirement was copied into the main spec, and archived artifacts were byte-compared with their merged versions before moving.

The Docker TLS deployment rehearsal passed locally on macOS using the production image build, PostgreSQL 16.14, and PowerDNS 5.1.3. It verified certificate validation, readiness, embedded console delivery, cookie attributes and authentication, an authoritative DNS write, cross-origin sign-in/write/sign-out rejection, unchanged DNS after a rejected write, logout replay rejection, and continued API-token access. It also checked actual port bindings, every DANS network's internal status, and denied direct ingress access to the upstream API. Credentials were absent from runtime logs.

The unchanged deployment first failed because Caddy's file capability was absent from its bounding set. After retaining only `NET_BIND_SERVICE`, Caddy started but TLS remained unreachable while ingress belonged only to an internal network. Giving only ingress an external-facing bridge made the complete rehearsal pass. DANS still drops all capabilities and joins only private networks.

An injected post-readiness failure returned the expected fixed phase summary. The exact CI cleanup assertions passed: no run-owned containers, volumes, networks, or image tag remained, and the generated secret directory was removed. Raw evidence remained private and was not staged or uploaded.

Shell syntax, deployment configuration rendering, the integration/diagnostic contracts, `git diff --check`, and strict OpenSpec validation passed. Baseline Go tests passed with the native Go 1.27.1 toolchain after permitting their local HTTP test listeners; the initial sandbox listener denial was not a code failure. No Go application code changed. Image compilation used the pinned Docker builder.

This evidence covers Docker Compose and API session transport. Hosted Linux execution, Podman runtime, browser rendering, and Kubernetes NetworkPolicy enforcement are not claimed as local results.

## Review

OCR review is pending. Raw review output and operational evidence remain outside the repository.
