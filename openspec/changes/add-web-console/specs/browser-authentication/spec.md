## Purpose

Provide persistent browser sign-in using existing API tokens while preserving current authority, credential confidentiality, and multi-instance revocation behavior.

## ADDED Requirements

### Requirement: Token-backed remembered browser sign-in
The console SHALL accept an existing active API token and establish browser authentication that survives reloads and browser restarts for at most seven days from sign-in. Authentication MUST end sooner when the original token expires or is revoked, its owner is disabled, or the browser session is invalidated. The system MUST NOT require an IdP, retain the submitted plaintext API token, or expose credentials to browser storage APIs, URLs, logs, or audit data.

#### Scenario: Sign in and reload
- **WHEN** an enabled identity submits its active API token and then reloads the console or restarts the browser
- **THEN** the browser remains authenticated within the allowed lifetime without pasting the token again
- **AND** authenticated API requests continue to evaluate current identity and authority

#### Scenario: Invalid sign-in
- **WHEN** the submitted token is malformed, unknown, expired, revoked, or owned by a disabled identity
- **THEN** sign-in returns the same HTTP 401 error shape without revealing credential state and establishes no usable session

#### Scenario: Absolute expiry
- **WHEN** seven days have elapsed since sign-in, even if the browser continues sending the old credential
- **THEN** the server rejects that browser credential without extending its lifetime through activity

### Requirement: Protected browser credential transport
Browser credentials SHALL by default be delivered only in Secure, HttpOnly, host-scoped cookies with an explicit same-site policy. An explicit server-configured local HTTP development mode MAY omit Secure using a distinct cookie name; it MUST preserve HttpOnly, same-site policy, session lifetime, current authority checks, and CSRF protection. Client-controlled Host or forwarding headers MUST NOT select this mode, and the default secure mode MUST NOT accept the development cookie name. State-changing browser requests, including sign-in, sign-out, refresh requests, and DNS or management mutations, MUST reject cross-origin request forgery before changing state. Authenticated responses MUST retain Cache-Control no-store. Cookies and other client credentials MUST NOT be forwarded to PowerDNS.

#### Scenario: Credential cannot be read by page scripts
- **WHEN** sign-in succeeds under the supported TLS ingress
- **THEN** the credential cookie in the default secure mode has Secure and HttpOnly attributes and is not returned as a script-readable secret

#### Scenario: Safari uses the local HTTP development console
- **GIVEN** the server explicitly enables development HTTP cookies
- **WHEN** a browser that rejects Secure cookies on loopback HTTP signs in
- **THEN** it receives a distinct host-scoped HttpOnly, SameSite=Strict cookie without Secure, can authenticate subsequent requests, and can sign out without weakening the default secure mode
- **AND** localStorage, sessionStorage, and IndexedDB contain no authentication credential

#### Scenario: Cross-origin write
- **WHEN** a cross-origin browser attempts a state-changing operation using ambient cookies
- **THEN** the operation is rejected before a DNS request or management mutation occurs

### Requirement: Browser sign-out preserves the original token
Sign-out SHALL clear the browser credential and invalidate that browser session without revoking the original API token or other browser sessions. The console MUST return to sign-in without retaining protected page data as an authenticated view.

#### Scenario: Sign out one browser
- **WHEN** a caller signs out
- **THEN** replay of that browser session is rejected on every instance
- **AND** the original unexpired, unrevoked API token remains usable by CLI clients

#### Scenario: Sign out an expired browser
- **WHEN** the browser credential has already expired or is unusable
- **THEN** the user can still clear it and return to sign-in without being trapped in an authenticated page

### Requirement: Browser sessions share current revocation state
Browser authentication and authorization SHALL work across same-version API instances without sticky routing or process-local authority caches. A revocation or disablement committed before the applicable authorization decision point MUST apply to that request; database unavailability MUST fail closed.

#### Scenario: Revoke the original token on another instance
- **WHEN** an operator revokes the original token through one instance before a browser request reaches its authorization decision point on another
- **THEN** the browser request is denied and sends no corresponding mutation to PowerDNS

#### Scenario: Change delegation while signed in
- **WHEN** a delegation is revoked while its grantee remains signed in
- **THEN** later browser writes cannot use that revoked authority even if a previously rendered control is still enabled

#### Scenario: Database unavailable
- **WHEN** current browser-session, token, or authority state cannot be established from the primary database
- **THEN** protected requests fail without using cached authentication or forwarding a DNS mutation
