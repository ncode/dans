## Purpose

Defines the availability, persistence, health, schema, and upgrade contract for safely operating one or more DANS API instances.

## ADDED Requirements

### Requirement: Same-version instances share current authorization state
DANS SHALL support two or more same-version API instances using one PostgreSQL primary and the same PowerDNS upstream. A request reaching its authorization decision point MUST use all relevant state committed before that point, regardless of which instance committed the state, and a later commit SHALL NOT retroactively change that completed decision.

#### Scenario: Revocation is enforced across instances
- **WHEN** an operator commits a revocation through one instance before a request reaches the authorization decision point on another instance
- **THEN** the second instance denies authority that depended on the revoked grant

#### Scenario: Revocation follows the documented decision boundary
- **WHEN** a request reaches its authorization decision point before a concurrent revocation commits
- **THEN** DANS does not retroactively cancel that request solely because the revocation committed afterward

### Requirement: PostgreSQL support has an explicit version window
PostgreSQL SHALL be the only supported durable state engine, and DANS SHALL support PostgreSQL majors 16, 17, and 18 on their current minor releases. An API instance MUST NOT report readiness when its database reports a major outside that range.

#### Scenario: Supported PostgreSQL major
- **WHEN** a DANS release connects to a current minor release of PostgreSQL 16, 17, or 18 with the compatible schema
- **THEN** database version compatibility does not prevent the instance from becoming ready

#### Scenario: Unsupported PostgreSQL major
- **WHEN** an instance connects to a PostgreSQL major outside 16 through 18
- **THEN** `/readyz` returns 503 and the instance does not serve authenticated API traffic

### Requirement: Schema changes are explicit and serialized
The API runtime SHALL NOT create or migrate its database schema at startup. A separately invoked migration command MUST apply embedded forward-only migrations using schema-change credentials, and concurrent migration invocations MUST NOT interleave schema changes or leave a partially ordered schema.

#### Scenario: Compatible schema at startup
- **WHEN** an API instance starts with runtime credentials and the exact compatible schema
- **THEN** it can become ready without issuing schema changes

#### Scenario: Missing or incompatible schema
- **WHEN** an API instance starts against a missing, older, or newer incompatible schema
- **THEN** `/readyz` returns 503 and authenticated requests fail without being forwarded to PowerDNS

#### Scenario: Concurrent migration attempts
- **WHEN** two operators invoke the migration command against the same database at the same time
- **THEN** migrations execute under mutual exclusion and the database ends in one complete ordered schema state

#### Scenario: Runtime credentials cannot migrate
- **WHEN** the migration command is invoked with the API runtime database credentials
- **THEN** it exits with failure and does not alter the schema

### Requirement: Dependency failures remain fail closed
DANS SHALL fail authenticated API operations when current authorization state cannot be read from the PostgreSQL primary, and it SHALL NOT bypass authorization, use stale authority, or forward such operations to PowerDNS. An unavailable PowerDNS upstream MUST produce a request failure rather than failover or queued work, and DANS MUST NOT automatically retry or replay a mutation whose outcome may be ambiguous.

#### Scenario: Primary database is unavailable
- **WHEN** the PostgreSQL primary is unavailable before an authorization decision
- **THEN** DANS returns a failure and sends no corresponding request to PowerDNS

#### Scenario: PowerDNS is unavailable
- **WHEN** an authorized request cannot reach the configured PowerDNS upstream
- **THEN** DANS returns a failure and does not queue the request for later forwarding

#### Scenario: Mutation result is ambiguous
- **WHEN** the upstream connection fails after DANS has forwarded a mutation and the applied result cannot be determined
- **THEN** DANS does not automatically retry or replay that mutation after connectivity recovers

### Requirement: Liveness and readiness expose distinct signals
DANS SHALL expose unauthenticated `/livez` and `/readyz` probes that return only status 200 or 503 without dependency details. `/livez` MUST reflect whether the process can serve HTTP, while `/readyz` MUST return 200 only when the instance can reach the PostgreSQL primary, sees a compatible schema and auditable storage path, has successfully authenticated to a compatible PowerDNS upstream, and is not draining.

#### Scenario: All readiness conditions pass
- **WHEN** the process is serving and every readiness dependency and compatibility check succeeds
- **THEN** `/livez` and `/readyz` both return 200

#### Scenario: A readiness dependency fails
- **WHEN** PostgreSQL, schema compatibility, audit persistence health, or the authenticated PowerDNS compatibility probe fails
- **THEN** `/livez` remains 200 while `/readyz` returns 503 without identifying the failed dependency

#### Scenario: Probe routes are anonymous
- **WHEN** a caller requests `/livez` or `/readyz` without an API token
- **THEN** DANS returns the applicable health status rather than an authentication error

### Requirement: Shutdown drains bounded in-flight work
On graceful termination, an instance SHALL become unready before it stops accepting new work, SHALL allow already accepted requests to finish for a configured bounded interval, and MUST exit when that interval ends.

#### Scenario: Graceful termination begins
- **WHEN** an instance receives its graceful termination signal
- **THEN** `/readyz` changes to 503, new work is refused, and accepted work is given the configured drain interval to finish

#### Scenario: Drain deadline expires
- **WHEN** an in-flight request remains after the configured drain interval
- **THEN** the instance terminates without waiting indefinitely

### Requirement: Runtime logs are structured and secret-safe
The runtime SHALL emit structured JSON access and error logs containing request IDs and opaque resource IDs needed for operational correlation. Logs MUST NOT contain API tokens, the PowerDNS key, secret-file contents, or request and response bodies.

#### Scenario: Authenticated request is logged
- **WHEN** DANS completes an authenticated request
- **THEN** its structured log identifies the request and relevant opaque resources without containing credentials or message bodies

### Requirement: The supported network boundary is private behind TLS ingress
The supported production topology SHALL terminate client TLS at a trusted ingress, keep the DANS listener private, and restrict the PowerDNS API listener and key so that DANS clients cannot reach them directly. Supplied deployment examples MUST preserve this boundary whether DANS reaches PowerDNS over a colocated socket or restricted private networking.

#### Scenario: Production example is deployed
- **WHEN** an operator applies a supplied production deployment example
- **THEN** clients reach DANS through the TLS ingress and only DANS can reach the PowerDNS API listener

### Requirement: Version upgrades are coordinated
DANS v1 SHALL support coordinated upgrades, not mixed-version rolling upgrades. The documented upgrade procedure MUST require a verified PostgreSQL backup, draining every API instance, running the target binary's forward-only migration, and starting only that target version; an older binary MUST NOT report readiness against a schema migrated for a newer incompatible version.

#### Scenario: Coordinated upgrade succeeds
- **WHEN** an operator backs up the database, drains all instances, runs the target migration, and starts only target-version instances
- **THEN** the target instances become ready and management traffic can resume without changing authoritative DNS availability

#### Scenario: Old binary starts after migration
- **WHEN** an operator starts an older incompatible binary after the target schema migration
- **THEN** that binary remains unready and does not serve authenticated API traffic

#### Scenario: Downgrade after migration
- **WHEN** an operator needs to return to an older version after a schema migration
- **THEN** in-place binary downgrade is unsupported and the documented recovery path requires restoring the matching verified backup before starting the older version
