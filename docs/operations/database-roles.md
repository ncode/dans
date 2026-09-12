# PostgreSQL roles

DANS uses separate PostgreSQL login roles:

- The DDL role owns the DANS database and schema. Only offline `dans db migrate` maintenance uses it.
- The runtime role is used by API instances and the bootstrap, recovery, and restore-finalization data-maintenance commands. It can read authorization state, perform the documented lifecycle writes, append audit events, remove direct group memberships, create/delete browser sessions, and maintain rebuildable browsing state. It cannot change the migration ledger, modify or delete audit events, hard-delete durable resources, or create schema objects.

Create the database and schema with the DDL role as owner. Do not grant the runtime role membership in the DDL role, schema ownership, `CREATE` on the database/schema, or any PostgreSQL superuser capability. Revoke `PUBLIC` access according to the deployment's database policy.

After running migrations with the DDL credential, apply the checked-in runtime grant matrix with `psql`:

```sh
psql "$DDL_DATABASE_URL" \
  --set=database_name=dans \
  --set=schema_name=public \
  --set=runtime_role=dans_runtime \
  --file=internal/database/privileges/runtime.sql
```

All three identifiers are explicit and are quoted by `psql`; substitute the deployment's actual database, schema, and runtime-role names. Reapply the grant file after each forward migration so new objects receive only reviewed privileges.

The API runtime checks `schema_migrations` but never creates or updates it. A migration attempted with the runtime credential must fail. PostgreSQL 16, 17, and 18 current minor releases are the supported server versions.

Browser sessions intentionally allow `SELECT`, `INSERT`, and `DELETE`, with no runtime `UPDATE`: their original-token association and absolute expiry cannot be extended in place. Browsing tables allow data maintenance, including deletion of obsolete staged generations. The grant script resets both PUBLIC and runtime-role privileges before granting these capabilities. These exceptions do not expand the runtime role's rights on the migration ledger, identity/token policy, or append-only audit history.
