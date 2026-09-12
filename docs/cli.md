# DANS CLI

The `dans` executable serves both people and automation. Every invocation builds a new command tree, resolves an immutable configuration, and runs without prompts or automatic mutation retries.

See [api.md](api.md) for the generated public Go client and [policy.md](policy.md) for delegation examples, route classes, and explicit v1 limitations.

## Configuration

`--config FILE` selects one JSON file. If the flag is absent, `DANS_CONFIG` may select it. DANS does not search for a configuration file and rejects unknown properties, non-string values, malformed JSON, and alternate formats.

```json
{
  "endpoint": "https://dns.example.net/api/v1",
  "output": "json",
  "api_token_file": "/run/secrets/dans-token"
}
```

Non-secret values resolve in this order: built-in default, selected JSON file, documented environment variable, command flag.

| JSON property | Environment | Flag | Default |
| --- | --- | --- | --- |
| `endpoint` | `DANS_ENDPOINT` | `--endpoint` | `http://127.0.0.1:8080/api/v1` |
| `output` | `DANS_OUTPUT` | `--output` | `text` |
| `listen` | `DANS_LISTEN` | `--listen` | `127.0.0.1:8080` |
| `browser_cookie_mode` | `DANS_BROWSER_COOKIE_MODE` | `--browser-cookie-mode` | `secure` |
| `api_token_file` | `DANS_API_TOKEN_FILE` | `--api-token-file` | none |
| `database_url_file` | `DANS_DATABASE_URL_FILE` | `--database-url-file` | none |
| `powerdns_url` | `DANS_POWERDNS_URL` | `--powerdns-url` | none |
| `powerdns_unix_socket` | `DANS_POWERDNS_UNIX_SOCKET` | `--powerdns-unix-socket` | none |
| `powerdns_upstream` | `DANS_POWERDNS_UPSTREAM` | `--powerdns-upstream` | `default` |
| `powerdns_timeout` | `DANS_POWERDNS_TIMEOUT` | `--powerdns-timeout` | `20s` |
| `powerdns_api_key_file` | `DANS_POWERDNS_API_KEY_FILE` | `--powerdns-api-key-file` | none |
| `powerdns_config_file` | `DANS_POWERDNS_CONFIG_FILE` | `--powerdns-config-file` | none |
| `database_max_connections` | `DANS_DATABASE_MAX_CONNECTIONS` | `--database-max-connections` | `12` |
| `database_authentication_reserve` | `DANS_DATABASE_AUTHENTICATION_RESERVE` | `--database-authentication-reserve` | `2` |
| `database_readiness_reserve` | `DANS_DATABASE_READINESS_RESERVE` | `--database-readiness-reserve` | `1` |
| `database_connect_timeout` | `DANS_DATABASE_CONNECT_TIMEOUT` | `--database-connect-timeout` | `5s` |
| `database_statement_timeout` | `DANS_DATABASE_STATEMENT_TIMEOUT` | `--database-statement-timeout` | `10s` |
| `database_lock_timeout` | `DANS_DATABASE_LOCK_TIMEOUT` | `--database-lock-timeout` | `2s` |
| `database_transaction_timeout` | `DANS_DATABASE_TRANSACTION_TIMEOUT` | `--database-transaction-timeout` | `15s` |
| `max_body_bytes` | `DANS_MAX_BODY_BYTES` | `--max-body-bytes` | `16777216` |
| `request_timeout` | `DANS_REQUEST_TIMEOUT` | `--request-timeout` | `30s` |
| `max_concurrent_requests` | `DANS_MAX_CONCURRENT_REQUESTS` | `--max-concurrent-requests` | `64` |
| `max_header_bytes` | `DANS_MAX_HEADER_BYTES` | `--max-header-bytes` | `1048576` |
| `read_header_timeout` | `DANS_READ_HEADER_TIMEOUT` | `--read-header-timeout` | `5s` |
| `read_timeout` | `DANS_READ_TIMEOUT` | `--read-timeout` | `35s` |
| `write_timeout` | `DANS_WRITE_TIMEOUT` | `--write-timeout` | `35s` |
| `idle_timeout` | `DANS_IDLE_TIMEOUT` | `--idle-timeout` | `60s` |
| `health_timeout` | `DANS_HEALTH_TIMEOUT` | `--health-timeout` | `5s` |
| `shutdown_timeout` | `DANS_SHUTDOWN_TIMEOUT` | `--shutdown-timeout` | `30s` |

Only these environment names are bound. Empty environment values are treated as unset. JSON configuration represents durations and integer limits as strings, matching their environment and flag forms.

`browser_cookie_mode` accepts `secure` or `development-http`. The latter is explicitly enabled by the loopback-only local Compose stack so browsers that reject Secure cookies over HTTP, including Safari, can sign in. It uses a separate `dans_dev_session` cookie without Secure; HttpOnly, SameSite=Strict, CSRF protection, expiry, and current authority checks remain enforced. Use the default `secure` mode behind HTTPS for deployed services. The mode is configured by the server operator and is never inferred from client-supplied Host or forwarding headers.

## Secrets

The DANS token, PostgreSQL URL, and PowerDNS key are never accepted as literal JSON properties or command flags. Supply exactly one source for each required secret:

| Secret | Environment value | File setting |
| --- | --- | --- |
| DANS API token | `DANS_API_TOKEN` | `api_token_file` |
| PostgreSQL URL | `DANS_DATABASE_URL` | `database_url_file` |
| PowerDNS API key | `DANS_POWERDNS_API_KEY` | `powerdns_api_key_file` |

The PowerDNS key may instead come from the explicitly named `powerdns_config_file`, which must contain exactly one plaintext `api-key=...` assignment. No PowerDNS configuration path is discovered automatically.

Secret files may be regular files or symlinks to regular mounted files. DANS reads each source once, removes one terminal LF or CRLF from file-backed secrets, and rejects empty, oversized, NUL-containing, control-character, or malformed values. Secret values are redacted from diagnostics. Environment secrets containing a newline are rejected.

## Online workflows

All online commands use the generated client for the public API. They never connect to PostgreSQL or PowerDNS directly.

```text
dans identities  list|create|get|update
dans identities  tokens list|create|revoke
dans groups      list|create|get|update
dans groups      members list|add|remove
dans delegations list|create|get|revoke
dans bindings    list|create|get|observe|confirm-absent|retry-delete|rebind
dans reconcile   ...                         # alias of bindings
dans audit       list|export
dans me          get|groups|delegations|tokens|token-create|token-revoke
dans zones       list|get|create|delete
dans rrsets      list|get|apply|replace|add-value|remove-value|delete
dans health      live|ready
dans docs
```

Create, update, and typed batch commands take `--data FILE`; use `--data -` to read one bounded strict JSON object from stdin. The JSON shape is the corresponding schema in `dans docs`. This is endpoint-specific typed input, not a raw-request command.

For example:

```sh
printf '%s\n' '{"handle":"alice","kind":"user"}' |
  dans --output json identities create --data -

dans rrsets add-value reverse.example. \
  10.2.0.192.in-addr.arpa. PTR host.example. --ttl 300
```

`rrsets add-value` and `rrsets remove-value` send one generated `EXTEND` or `PRUNE` patch. `rrsets apply` accepts a complete typed `ZonePatch` batch, while `rrsets replace` submits the complete replacement value set. Rare PowerDNS operations remain available through the published generated client and HTTP API; there is intentionally no generic raw-request command.

Zone and RRset deletion require `--confirm` and never prompt. If confirmation is absent, no request is sent.

## Output and exit status

`--output text` emits a deterministic human-readable representation. `--output json` emits one compact JSON value and is the stable automation format. `audit export` always emits NDJSON: one complete audit object per line without an enclosing array.

Successful data is written only to stdout. Diagnostics are written only to stderr. Runtime and API failures do not append command usage.

| Exit status | Meaning |
| --- | --- |
| `0` | success |
| `1` | runtime, transport, or API failure |
| `2` | invocation or configuration failure |
| `130` | interrupted or canceled |

Help, version, and completion are configuration-free and perform no file, network, database, or PowerDNS work:

```sh
dans help
dans version
dans completion bash
```

## Serving the API

`dans serve` requires the PostgreSQL URL and PowerDNS API key secret sources described above. Configure exactly one PowerDNS transport: `powerdns_url` for restricted HTTP(S), or `powerdns_unix_socket` for a clean absolute socket path when colocated. The stable `powerdns_upstream` name identifies that single configured upstream in bindings and audit data.

The command validates every pool, request, connection, health, and shutdown bound before opening dependencies. It reads the selected JSON file, environment, flags, and secret files once, then keeps that immutable configuration until restart. Cancellation marks readiness false before draining accepted work for `shutdown_timeout`; interruption exits with status 130. Runtime and access logs are JSON on stderr and exclude credentials and bodies.

## Offline maintenance

The release executable also owns the database lifecycle and credential-recovery workflows:

```text
dans db migrate
dans db status
dans bootstrap --handle HANDLE --display-name NAME --token-label LABEL
dans recover operator-token (--identity-id ID | --handle HANDLE) --token-label LABEL
dans restore finalize (--identity-id ID | --handle HANDLE) --token-label LABEL --confirm
```

These commands use `DANS_DATABASE_URL` or `database_url_file` and never call the public HTTP API. Bootstrap only initializes an empty installation. Recovery only targets an existing enabled operator. Restore finalization requires `--confirm`, revokes all restored tokens, and prints the single replacement credential only once to stdout.

## Console sign-in and CLI credentials

The embedded browser console uses the same public API with a token-backed browser session. Existing CLI commands continue using `X-API-Key`; signing out of the console does not revoke that CLI token. Revoking a token through the CLI or console ends all browser sessions associated with it. The console marks the token backing the current sign-in and confirms revocation consequences. Identity, group, membership, token, and binding administration are also available in the console under the same authority rules. See [the management and browser API](api.md).
