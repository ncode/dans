\set ON_ERROR_STOP on

\if :{?database_name}
\else
DO $$ BEGIN RAISE EXCEPTION 'database_name is required'; END $$;
\endif
\if :{?schema_name}
\else
DO $$ BEGIN RAISE EXCEPTION 'schema_name is required'; END $$;
\endif
\if :{?runtime_role}
\else
DO $$ BEGIN RAISE EXCEPTION 'runtime_role is required'; END $$;
\endif

GRANT CONNECT ON DATABASE :"database_name" TO :"runtime_role";
GRANT USAGE ON SCHEMA :"schema_name" TO :"runtime_role";
REVOKE CREATE ON SCHEMA :"schema_name" FROM PUBLIC, :"runtime_role";

REVOKE ALL PRIVILEGES ON TABLE
    :"schema_name".schema_migrations,
    :"schema_name".installation_metadata,
    :"schema_name".operator_guard,
    :"schema_name".identities,
    :"schema_name".api_tokens,
    :"schema_name".groups,
    :"schema_name".group_memberships,
    :"schema_name".zone_bindings,
    :"schema_name".delegations,
    :"schema_name".delegation_selectors,
    :"schema_name".delegation_record_types,
    :"schema_name".delegation_change_kinds,
    :"schema_name".audit_events
FROM PUBLIC, :"runtime_role";

GRANT SELECT ON TABLE
    :"schema_name".schema_migrations,
    :"schema_name".installation_metadata,
    :"schema_name".operator_guard,
    :"schema_name".identities,
    :"schema_name".api_tokens,
    :"schema_name".groups,
    :"schema_name".group_memberships,
    :"schema_name".zone_bindings,
    :"schema_name".delegations,
    :"schema_name".delegation_selectors,
    :"schema_name".delegation_record_types,
    :"schema_name".delegation_change_kinds,
    :"schema_name".audit_events
TO :"runtime_role";

GRANT INSERT ON TABLE
    :"schema_name".installation_metadata,
    :"schema_name".identities,
    :"schema_name".api_tokens,
    :"schema_name".groups,
    :"schema_name".group_memberships,
    :"schema_name".zone_bindings,
    :"schema_name".delegations,
    :"schema_name".delegation_selectors,
    :"schema_name".delegation_record_types,
    :"schema_name".delegation_change_kinds,
    :"schema_name".audit_events
TO :"runtime_role";

GRANT UPDATE (singleton)
    ON TABLE :"schema_name".operator_guard TO :"runtime_role";
GRANT UPDATE (restore_required, restore_finalized_at)
    ON TABLE :"schema_name".installation_metadata TO :"runtime_role";
GRANT UPDATE (display_name, enabled, is_operator, updated_at)
    ON TABLE :"schema_name".identities TO :"runtime_role";
GRANT UPDATE (revoked_at)
    ON TABLE :"schema_name".api_tokens TO :"runtime_role";
GRANT UPDATE (display_name, enabled, updated_at)
    ON TABLE :"schema_name".groups TO :"runtime_role";
GRANT UPDATE (retired_at, retired_by_identity_id)
    ON TABLE :"schema_name".zone_bindings TO :"runtime_role";
GRANT UPDATE (revoked_at, revoked_by_identity_id)
    ON TABLE :"schema_name".delegations TO :"runtime_role";

GRANT DELETE ON TABLE :"schema_name".group_memberships TO :"runtime_role";

-- Browser sessions and rebuildable browsing state are owned by this runtime.
REVOKE ALL PRIVILEGES ON TABLE :"schema_name".browser_sessions, :"schema_name".browse_zones, :"schema_name".browse_rrsets, :"schema_name".browse_changes FROM PUBLIC, :"runtime_role";
GRANT SELECT, INSERT, DELETE ON TABLE :"schema_name".browser_sessions TO :"runtime_role";
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE :"schema_name".browse_zones, :"schema_name".browse_rrsets, :"schema_name".browse_changes TO :"runtime_role";
