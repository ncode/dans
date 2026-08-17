# Use a database snapshot, not distributed request locks

Each request authenticates and computes current authority in one primary-PostgreSQL statement whose snapshot is its authorization decision point; DANS uses no permission cache, read replica, or global advisory lock. It commits authorization-dependent audit intent before contacting PowerDNS and never holds a database transaction or lock across the upstream call, so concurrent requests retain PowerDNS's last-successful-write behavior and a request already authorized may finish after revocation or zone retirement.
