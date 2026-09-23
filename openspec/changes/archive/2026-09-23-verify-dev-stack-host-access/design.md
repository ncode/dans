## Context

See [proposal.md](proposal.md). `scripts/dev-stack.sh` already creates a unique delegated-write fixture and checks its DNS/audit effects. Its HTTP commands execute inside the application container, and its DNS query uses an internal service name. The quickstart intentionally requires only Docker Compose v2 and Make on macOS/Linux.

## Goals / Non-Goals

**Goals:** Distinguish internal-stack health from reachability through the developer's published loopback ports while reusing the existing fixture.

**Non-Goals:** Full browser end-to-end testing, new public listeners, network auto-configuration, or mandatory new quickstart dependencies.

## Decisions

### Add one optional target

Expose `make smoke-host` through the current lifecycle script. Check host `curl` and `dig` availability before creating resources, then run the existing smoke flow once and use that invocation's known owner/type/value for host checks. Retain the fixture as the current smoke command does. Update `make help` and a short README reference in this change so the target is discoverable as soon as it lands.

Adding host tools to ordinary `make smoke` would break the agreed prerequisite contract. Container-only probes, including probes of a Docker VM's loopback interface, cannot prove reachability from the actual developer host.

### Probe the advertised host interfaces

Run HTTP probes in the host shell against `127.0.0.1` and the port resolved by `docker compose port`, so shell overrides and Compose `.env` values use the same effective publication. Require HTTP 200 and the expected readiness result at `/readyz`; require HTTP 200 and the actual console shell at `/console/`. Bypass configured HTTP proxies for these loopback requests. A successful TCP connection or a generic error page is insufficient.

Use host `dig` against `127.0.0.1` and protocol-specific ports resolved by `docker compose port`. Make separate nonrecursive UDP and TCP requests and validate successful authoritative responses with the exact fixture owner, A type, and value. Disable TCP fallback for the UDP probe so a broken UDP publication cannot pass through TCP. Do not query a default resolver or an internal container address.

Apply explicit connection/request timeouts and a fixed retry bound. Report the failing protocol and effective port without credentials, request headers, or machine-specific details. Do not read the operator token for the host HTTP probes: internal smoke already exercises authenticated writes, and readiness/console delivery are public.

### Verify failure detection, not only success

Run on Linux and macOS with both default and overridden ports. In disposable runs, keep internal services healthy while individually breaking HTTP, UDP DNS, and TCP DNS publication; each corresponding host check must fail. Test wrong HTTP content, wrong DNS answers, tool absence, and unreachable endpoints with a small command-boundary test and focused live cases. Do not add production fault routes.

## Risks / Trade-offs

- `dig` output varies → Parse only stable DNS status, flags, and answer fields; accept irrelevant additional records and ignore ordering.
- A developer lacks probe tools → Fail before fixture creation with a prerequisite hint; ordinary smoke remains available.
- Local port conflicts → Honor existing overrides and distinguish host failure from internal readiness failure.

## Migration Plan

Add the optional target and its tests without changing default quickstart behavior. Lifecycle CI adopts it in its own change. Rollback removes the optional target and host probes only; persistent data needs no migration.
