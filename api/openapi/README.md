# OpenAPI contract

`vendor/powerdns-authoritative-5.1.3.yaml` is the unmodified file from PowerDNS tag
`auth-5.1.3`:

<https://raw.githubusercontent.com/PowerDNS/pdns/auth-5.1.3/docs/http-api/openapi/authoritative-api-openapi.yaml>

Its recorded SHA-256 is
`9b224e5b2456648503efd367bf9724325bb075337dd18ecadd31df6792b8d636`.
Run `make openapi-source-verify` to check it or `make openapi-source-update` to
refresh the file and checksum from that exact tag.

`dans-overlay.yaml` adds the DANS routes and access classes. It also:

- uses `ZonePatch` for zone `PATCH`, allowing TTL and records to be absent for
  the PowerDNS-supported `DELETE`, `EXTEND`, and `PRUNE` request shapes;
- adds the JSON request bodies omitted by the published schema for metadata and
  cryptokey `PUT`; the PowerDNS 5.1.3 handlers call `req->json()` and require
  `Metadata` and `Cryptokey` payloads respectively;
- moves the published `Record.modified_at` sibling into `Record.properties`;
- models required nullable DANS response fields with OpenAPI 3.1 string/null
  unions so generated response types can encode JSON `null`;
- renames only Go-generator collisions without changing HTTP paths.

`make generate` writes the combined `openapi.json` and the importable generated
Go client/models/server surface in `github.com/ncode/dans/api`.
`make generate-check` fails when either checked-in artifact is stale.
