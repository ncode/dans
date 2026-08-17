# DANS

DNS Authorization and Name Service is a PostgreSQL-backed policy gateway for PowerDNS Authoritative. It lets identities and groups share a zone while limiting non-operator writes to exact or Route 53-style glob matches over whole RRsets, including PTR records, without creating subzones.

DANS exposes the pinned PowerDNS 5.1 API plus its own identity, group, token, delegation, binding, lifecycle, and audit resources. One `dans` executable serves the API, runs database maintenance, and dogfoods the generated public client for management and common DNS workflows.

## Documentation

- [Public API and generated Go client](docs/api.md)
- [CLI and immutable configuration](docs/cli.md)
- [Policy examples, route classes, and v1 limitations](docs/policy.md)
- [Operations runbooks](docs/operations/runbooks.md)
- [Docker/Podman and Kubernetes examples](deploy/README.md)
- [Release artifacts](docs/release.md)
- [Measured performance and resource baseline](openspec/changes/build-dans-v1/evidence/performance/README.md)
- [Combined OpenAPI source](api/openapi/README.md)
- [Architecture decisions](docs/adr/0001-expose-a-classified-powerdns-api.md)

The supported production topology places private DANS instances behind a trusted TLS ingress, connects them to one PostgreSQL 16–18 primary and one PowerDNS `>=5.1.3,<5.2` API, and prevents clients from reaching the PowerDNS API/key directly. Authoritative DNS serving remains independent of DANS availability.
