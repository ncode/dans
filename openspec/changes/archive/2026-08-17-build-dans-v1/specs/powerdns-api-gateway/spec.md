## Purpose

Defines the authenticated, fail-closed compatibility gateway through which DANS exposes one supported PowerDNS Authoritative API without leaking credentials or changing upstream results.

## ADDED Requirements

### Requirement: Pinned PowerDNS compatibility surface
DANS SHALL expose the vendored PowerDNS Authoritative `/api/v1` contract to exactly one configured upstream whose reported version is `>=5.1.3` and `<5.2`. DANS MUST treat PowerDNS metrics, web UI, and undocumented routes as outside the compatibility surface, and it MUST serve the combined PowerDNS and DANS OpenAPI contract at authenticated `/api/docs`.

#### Scenario: Compatible upstream
- **WHEN** the configured upstream reports PowerDNS Authoritative version 5.1.3 or another version in the supported range
- **THEN** DANS can become ready to serve the compatibility surface

#### Scenario: Incompatible upstream
- **WHEN** the configured upstream reports a version outside the supported range
- **THEN** DANS remains unready, returns a service-unavailable error for compatibility requests, and forwards no request to that upstream

#### Scenario: Route outside the compatibility surface
- **WHEN** a caller requests PowerDNS `/metrics`, its web UI, or an undocumented route through DANS
- **THEN** DANS returns a not-found error and does not forward the request

#### Scenario: Authenticated combined API documentation
- **WHEN** an authenticated caller requests `/api/docs`
- **THEN** DANS returns the combined contract containing the supported PowerDNS operations and the DANS `/api/v1/dans` operations

### Requirement: Exhaustive operation authorization classes
DANS MUST assign every operation in the compatibility surface to exactly one route class. `GET` zone list, `GET` zone, `GET` zone export, and `GET` search-data operations SHALL be authenticated reads; zone `PATCH` SHALL be the only delegated RRset write; every other PowerDNS operation SHALL be operator-only. An operation without an explicit class MUST fail closed.

#### Scenario: Ordinary DNS read without a delegation
- **WHEN** an enabled authenticated identity without a delegation lists zones, gets a zone, exports a zone, or searches DNS data
- **THEN** DANS forwards the request as an authenticated read without redacting the compatible upstream response

#### Scenario: Delegated zone patch
- **WHEN** a non-operator submits a zone `PATCH`
- **THEN** DANS applies the RRset-delegation authorization contract before deciding whether to forward the request

#### Scenario: Operator-only operation
- **WHEN** a non-operator requests configuration, statistics, metadata, keys, server operations, or any PowerDNS mutation other than zone `PATCH`
- **THEN** DANS returns a forbidden error and sends no upstream request

#### Scenario: Unclassified declared operation
- **WHEN** the served compatibility contract contains an operation without an explicit route class
- **THEN** DANS remains unready rather than assigning an implicit class

### Requirement: Contract-faithful request handling
DANS SHALL validate each declared path, query, header, and request body against the combined contract before authorization or forwarding. It MUST reject malformed JSON with `400 Bad Request`, reject contract-invalid input and unknown request properties with `422 Unprocessable Entity`, authorize the same decoded request content that it sends upstream, and send no more than one upstream request for one accepted client request.

#### Scenario: Malformed JSON
- **WHEN** a declared JSON operation receives a syntactically invalid JSON body
- **THEN** DANS returns `400 Bad Request` in the DANS error format and sends no upstream request

#### Scenario: Contract-invalid request
- **WHEN** a request has an unknown property, a missing required value, or a value that violates the declared request schema
- **THEN** DANS returns `422 Unprocessable Entity` in the DANS error format and sends no upstream request

#### Scenario: Request is invalid and unauthenticated
- **WHEN** a request is both contract-invalid and lacks a valid DANS credential
- **THEN** DANS returns the contract-validation error before performing authorization or forwarding

#### Scenario: Authorized values equal forwarded values
- **WHEN** DANS accepts a request whose path, query, or JSON representation can be normalized without changing its meaning
- **THEN** the values authorized by DANS are semantically identical to the values in the single upstream request

### Requirement: Evidence-backed PowerDNS request compatibility
DANS SHALL accept legal PowerDNS 5.1.3 requests even where the published upstream request schema is inconsistent with the same release's documented behavior. Compatibility corrections MUST be limited to evidenced discrepancies and MUST NOT create an open-ended pass-through for undeclared input.

#### Scenario: RRset deletion without replacement fields
- **WHEN** a zone `PATCH` contains a valid `DELETE` change with an owner name and record type but no TTL or records array
- **THEN** DANS accepts the request shape for subsequent authorization instead of rejecting fields that PowerDNS does not require

#### Scenario: Individual-value change without TTL
- **WHEN** a zone `PATCH` contains a valid `EXTEND` or `PRUNE` change with records but no TTL
- **THEN** DANS accepts the request shape for subsequent authorization

#### Scenario: Undeclared extension remains rejected
- **WHEN** a client adds an input property that is neither in the pinned contract nor an evidenced compatibility correction
- **THEN** DANS returns `422 Unprocessable Entity` and sends no upstream request

### Requirement: Credential and forwarding-header isolation
DANS SHALL authenticate clients with the `X-API-Key` header containing a DANS API token. It MUST remove the client credential and all client-supplied hop-by-hop or forwarding headers before contacting PowerDNS, MUST authenticate upstream requests with only the configured PowerDNS API key, and MUST NOT expose either credential in a response or error. Every authenticated response MUST include `Cache-Control: no-store`.

#### Scenario: Client credential replacement
- **WHEN** an authenticated request is forwarded
- **THEN** PowerDNS receives the configured upstream API key and does not receive the caller's DANS token

#### Scenario: Spoofed forwarding headers
- **WHEN** a caller supplies `Forwarded`, `X-Forwarded-*`, `Connection`, or a header named by `Connection`
- **THEN** DANS removes those values before making the upstream request

#### Scenario: Missing or invalid client token
- **WHEN** `X-API-Key` is missing or does not identify an enabled DANS identity
- **THEN** DANS returns `401 Unauthorized`, exposes no credential detail, and sends no upstream request

#### Scenario: Authenticated response caching
- **WHEN** DANS returns any response to an authenticated request
- **THEN** the response contains `Cache-Control: no-store`

### Requirement: Upstream response fidelity
DANS SHALL return the upstream status code, body bytes, and end-to-end response headers without wrapping, decoding, schema-validating, or redacting them. It MUST remove hop-by-hop response headers and MUST apply only DANS-owned security or tracing headers in addition to the preserved upstream response.

#### Scenario: Successful upstream response
- **WHEN** PowerDNS returns a successful response, including a zone export or a no-content mutation response
- **THEN** DANS returns the same status and body with the upstream end-to-end headers preserved

#### Scenario: Upstream error response
- **WHEN** PowerDNS returns a `4xx` or `5xx` response
- **THEN** DANS returns that upstream status, body, and end-to-end headers without replacing it with a DANS error

#### Scenario: Upstream adds an undeclared response field
- **WHEN** a compatible PowerDNS response contains a field not modeled by the pinned response schema
- **THEN** DANS passes the field through unchanged

### Requirement: DANS-originated gateway errors
A DANS-originated gateway error MUST use JSON media type and the PowerDNS-compatible error object with a required human-readable `error` string and an optional `errors` string array. It MUST NOT include DANS credentials, the PowerDNS API key, internal storage details, or an upstream connection target.

#### Scenario: Authentication or authorization rejection
- **WHEN** DANS rejects a request because authentication is missing or invalid, or because the authenticated identity lacks the route's authority
- **THEN** DANS returns respectively `401 Unauthorized` or `403 Forbidden` using the compatible error object and sends no upstream request

#### Scenario: Upstream connection failure
- **WHEN** an authorized request cannot establish or maintain an upstream connection before receiving a response
- **THEN** DANS returns `502 Bad Gateway` using the compatible error object and does not retry the request automatically

#### Scenario: Upstream deadline expires
- **WHEN** the upstream deadline expires without an observed PowerDNS response
- **THEN** DANS returns `504 Gateway Timeout` using the compatible error object and does not retry or fail over the request automatically

#### Scenario: Authorization dependency unavailable
- **WHEN** DANS cannot authenticate or compute authority from current durable state
- **THEN** DANS returns `503 Service Unavailable`, performs no bypass, and sends no upstream request
