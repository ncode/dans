---
status: superseded by ADR-0005
---

# Run as a fail-closed control plane

DANS will begin as a low-volume, single-active control plane for one PowerDNS upstream, preferably colocated and connected over a Unix socket. Authorization-state failure or upstream unavailability never causes bypass, queuing, mutation retry, or failover; existing authoritative DNS keeps serving while management requests fail. Durable state, encrypted backups, token invalidation after stale restores, and audit records for security-sensitive activity are required before operation.
