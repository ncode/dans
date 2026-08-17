## Why

PowerDNS Authoritative protects its HTTP API with one server-wide key, so it cannot safely let multiple users and services share a zone while controlling only selected RRsets. DANS v1 will become the mandatory policy boundary, providing Route 53-style name delegation without forcing operators to create subzones.

## What Changes

- Add a contract-faithful gateway for the pinned PowerDNS Authoritative 5.1 API, with exhaustive per-operation access classes and fail-closed version handling.
- Add allow-only RRset delegations to identities or groups using exact and glob owner-name selectors plus optional record-type and change-kind restrictions.
- Add DANS-managed human and service identities, non-nested groups, operator roles, and revocable opaque API tokens.
- Add immutable zone-generation bindings and revoke-before-delete behavior so recreated zones never inherit old authority.
- Add durable, append-only audit intents and outcomes for DNS mutations and audit records for management changes and denials.
- Add PostgreSQL-backed multi-instance operation, explicit migrations, health checks, coordinated upgrades, and fail-closed dependency behavior.
- Add one generated combined OpenAPI client and one Cobra/Viper CLI that dogfoods every DANS management workflow and common DNS operations.
- Add required integration QA using real PostgreSQL, two DANS instances, and PowerDNS 5.1.3.
- Add reproducible efficient-go benchmarks and release/runtime footprint budgets backed by checked-in measurements.

## Capabilities

### New Capabilities

- `powerdns-api-gateway`: Expose and classify the pinned PowerDNS API while preserving compatible responses and protecting sensitive operations.
- `rrset-delegation`: Define, evaluate, and revoke user/group authority over matching RRsets and whole mutation batches.
- `identity-access-management`: Manage identities, groups, memberships, operator authority, and opaque API-token lifecycles.
- `audit-zone-lifecycle`: Persist mutation intent/outcome history and prevent authority from surviving zone deletion or recreation.
- `multi-instance-runtime`: Coordinate durable authorization state, schema lifecycle, health, and failure behavior across DANS instances.
- `cli-configuration-qa`: Provide deterministic configuration and CLI contracts and prove public behavior through dogfooded integration QA.
- `performance-resource-baseline`: Measure and bound release size, startup, ready-idle resources, and representative request paths without adding production observability surface.

### Modified Capabilities

None. This is the initial specification for a greenfield service.

## Impact

- Introduces the DANS Go service, combined OpenAPI contract, generated clients and handlers, PostgreSQL schema and migrations, CLI, container artifact, and integration environment.
- Adds runtime dependencies on PostgreSQL 16–18 and one PowerDNS Authoritative 5.1 upstream (`>=5.1.3,<5.2`).
- Adds pinned Go dependencies and tools including Cobra, Viper, `oapi-codegen`, `pgx/v5`, `sqlc`, and OpenAPI request validation.
- Adds checked-in benchmark and footprint evidence plus explicit release-footprint gates; it adds no production metrics or profiling endpoint.
- Requires PowerDNS API reachability and its upstream key to be restricted to DANS; existing authoritative DNS serving remains independent of DANS availability.
