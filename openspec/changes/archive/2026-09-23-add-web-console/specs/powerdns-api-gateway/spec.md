## MODIFIED Requirements

### Requirement: Credential and forwarding-header isolation
DANS SHALL authenticate clients with an X-API-Key header containing a DANS API token or with the explicitly supported token-backed browser credential. It MUST remove client API credentials, cookies, and all client-supplied hop-by-hop or forwarding headers before contacting PowerDNS, MUST authenticate upstream requests with only the configured PowerDNS API key, and MUST NOT expose either credential in a response or error. Every authenticated response MUST include Cache-Control no-store. Browser support MUST NOT change the pinned upstream route classes or make new browser/static routes into upstream passthroughs.

#### Scenario: Client credential replacement
- **WHEN** an authenticated request is forwarded
- **THEN** PowerDNS receives the configured upstream API key and does not receive the caller's DANS token

#### Scenario: Spoofed forwarding headers
- **WHEN** a caller supplies Forwarded, X-Forwarded-*, Connection, or a header named by Connection
- **THEN** DANS removes those values before making the upstream request

#### Scenario: Missing or invalid client token
- **WHEN** neither the supported header credential nor browser credential establishes an enabled DANS identity
- **THEN** DANS returns 401 Unauthorized, exposes no credential detail, and sends no upstream request

#### Scenario: Authenticated response caching
- **WHEN** DANS returns any response to an authenticated request
- **THEN** the response contains Cache-Control no-store

#### Scenario: Browser credentials remain local
- **WHEN** a browser-authenticated caller issues an authorized PowerDNS operation
- **THEN** the upstream request contains no browser cookie, session secret, or original API token
- **AND** the compatibility response retains its existing status/body/header fidelity
