CREATE TABLE installation_metadata (
    singleton boolean PRIMARY KEY DEFAULT true,
    installation_id uuid NOT NULL UNIQUE,
    initialized_at timestamptz NOT NULL DEFAULT now(),
    restore_required boolean NOT NULL DEFAULT false,
    restore_finalized_at timestamptz,
    CONSTRAINT installation_metadata_singleton CHECK (singleton),
    CONSTRAINT installation_metadata_restore_state CHECK (
        (restore_required AND restore_finalized_at IS NULL)
        OR (NOT restore_required)
    )
);

CREATE TABLE operator_guard (
    singleton boolean PRIMARY KEY DEFAULT true,
    CONSTRAINT operator_guard_singleton CHECK (singleton)
);

INSERT INTO operator_guard (singleton) VALUES (true);

CREATE TABLE identities (
    id uuid PRIMARY KEY,
    kind text NOT NULL,
    handle text COLLATE "C" NOT NULL UNIQUE,
    display_name text,
    enabled boolean NOT NULL DEFAULT true,
    is_operator boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT identities_kind CHECK (kind IN ('user', 'service')),
    CONSTRAINT identities_handle CHECK (
        octet_length(handle) BETWEEN 1 AND 63
        AND handle ~ '^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT identities_timestamps CHECK (updated_at >= created_at)
);

CREATE TABLE api_tokens (
    id uuid PRIMARY KEY,
    identity_id uuid NOT NULL REFERENCES identities (id) ON DELETE RESTRICT,
    label text COLLATE "C" NOT NULL,
    digest bytea NOT NULL UNIQUE,
    expires_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_tokens_label CHECK (
        octet_length(label) BETWEEN 1 AND 63
        AND label ~ '^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT api_tokens_digest CHECK (octet_length(digest) = 32),
    CONSTRAINT api_tokens_expiry CHECK (expires_at IS NULL OR expires_at > created_at),
    CONSTRAINT api_tokens_revocation CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE UNIQUE INDEX api_tokens_unrevoked_label
    ON api_tokens (identity_id, label)
    WHERE revoked_at IS NULL;

CREATE INDEX api_tokens_identity_created
    ON api_tokens (identity_id, created_at DESC, id DESC);

CREATE TABLE groups (
    id uuid PRIMARY KEY,
    handle text COLLATE "C" NOT NULL UNIQUE,
    display_name text,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT groups_handle CHECK (
        octet_length(handle) BETWEEN 1 AND 63
        AND handle ~ '^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT groups_timestamps CHECK (updated_at >= created_at)
);

CREATE TABLE group_memberships (
    group_id uuid NOT NULL REFERENCES groups (id) ON DELETE RESTRICT,
    identity_id uuid NOT NULL REFERENCES identities (id) ON DELETE RESTRICT,
    added_by_identity_id uuid NOT NULL REFERENCES identities (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, identity_id)
);

CREATE INDEX group_memberships_identity
    ON group_memberships (identity_id, group_id);

CREATE TABLE zone_bindings (
    id uuid PRIMARY KEY,
    generation bigint NOT NULL,
    upstream text COLLATE "C" NOT NULL,
    powerdns_zone_id text COLLATE "C" NOT NULL,
    zone_name text COLLATE "C" NOT NULL,
    created_by_identity_id uuid NOT NULL REFERENCES identities (id) ON DELETE RESTRICT,
    retired_by_identity_id uuid REFERENCES identities (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    retired_at timestamptz,
    CONSTRAINT zone_bindings_upstream CHECK (upstream <> ''),
    CONSTRAINT zone_bindings_generation CHECK (generation > 0),
    CONSTRAINT zone_bindings_powerdns_zone_id CHECK (powerdns_zone_id <> ''),
    CONSTRAINT zone_bindings_zone_name CHECK (
        zone_name <> ''
        AND right(zone_name, 1) = '.'
        AND zone_name = lower(zone_name)
    ),
    CONSTRAINT zone_bindings_retirement CHECK (
        (retired_at IS NULL AND retired_by_identity_id IS NULL)
        OR (retired_at >= created_at AND retired_by_identity_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX zone_bindings_generation
    ON zone_bindings (upstream, zone_name, generation);

CREATE UNIQUE INDEX zone_bindings_active_zone_id
    ON zone_bindings (upstream, powerdns_zone_id)
    WHERE retired_at IS NULL;

CREATE UNIQUE INDEX zone_bindings_active_zone_name
    ON zone_bindings (upstream, zone_name)
    WHERE retired_at IS NULL;

CREATE INDEX zone_bindings_zone_history
    ON zone_bindings (upstream, zone_name, created_at DESC, id DESC);

CREATE TABLE delegations (
    id uuid PRIMARY KEY,
    zone_binding_id uuid NOT NULL REFERENCES zone_bindings (id) ON DELETE RESTRICT,
    grantee_identity_id uuid REFERENCES identities (id) ON DELETE RESTRICT,
    grantee_group_id uuid REFERENCES groups (id) ON DELETE RESTRICT,
    created_by_identity_id uuid NOT NULL REFERENCES identities (id) ON DELETE RESTRICT,
    revoked_by_identity_id uuid REFERENCES identities (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CONSTRAINT delegations_one_grantee CHECK (
        (grantee_identity_id IS NOT NULL) <> (grantee_group_id IS NOT NULL)
    ),
    CONSTRAINT delegations_revocation CHECK (
        (revoked_at IS NULL AND revoked_by_identity_id IS NULL)
        OR (revoked_at >= created_at AND revoked_by_identity_id IS NOT NULL)
    )
);

CREATE INDEX delegations_binding_active
    ON delegations (zone_binding_id, id)
    WHERE revoked_at IS NULL;

CREATE INDEX delegations_identity_active
    ON delegations (grantee_identity_id, zone_binding_id)
    WHERE revoked_at IS NULL AND grantee_identity_id IS NOT NULL;

CREATE INDEX delegations_group_active
    ON delegations (grantee_group_id, zone_binding_id)
    WHERE revoked_at IS NULL AND grantee_group_id IS NOT NULL;

CREATE TABLE delegation_selectors (
    delegation_id uuid NOT NULL REFERENCES delegations (id) ON DELETE RESTRICT,
    position smallint NOT NULL,
    kind text NOT NULL,
    selector text COLLATE "C" NOT NULL,
    sql_like_pattern text COLLATE "C" NOT NULL,
    PRIMARY KEY (delegation_id, position),
    UNIQUE (delegation_id, kind, selector),
    CONSTRAINT delegation_selectors_position CHECK (position > 0),
    CONSTRAINT delegation_selectors_kind CHECK (kind IN ('exact', 'glob')),
    CONSTRAINT delegation_selectors_name CHECK (
        selector <> ''
        AND right(selector, 1) = '.'
        AND selector = lower(selector)
    ),
    CONSTRAINT delegation_selectors_like_pattern CHECK (sql_like_pattern <> '')
);

CREATE TABLE delegation_record_types (
    delegation_id uuid NOT NULL REFERENCES delegations (id) ON DELETE RESTRICT,
    record_type text COLLATE "C" NOT NULL,
    PRIMARY KEY (delegation_id, record_type),
    CONSTRAINT delegation_record_types_value CHECK (
        octet_length(record_type) BETWEEN 1 AND 32
        AND record_type ~ '^[A-Z][A-Z0-9-]*$'
    )
);

CREATE TABLE delegation_change_kinds (
    delegation_id uuid NOT NULL REFERENCES delegations (id) ON DELETE RESTRICT,
    change_kind text COLLATE "C" NOT NULL,
    PRIMARY KEY (delegation_id, change_kind),
    CONSTRAINT delegation_change_kinds_value CHECK (
        change_kind IN ('REPLACE', 'DELETE', 'EXTEND', 'PRUNE')
    )
);

CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    event_kind text NOT NULL,
    operation_id uuid,
    intent_event_id uuid,
    intent_event_kind text,
    request_id text COLLATE "C" NOT NULL,
    actor_identity_id uuid REFERENCES identities (id) ON DELETE RESTRICT,
    actor_token_id uuid REFERENCES api_tokens (id) ON DELETE RESTRICT,
    action text COLLATE "C" NOT NULL,
    target_kind text COLLATE "C" NOT NULL,
    target_id text COLLATE "C" NOT NULL,
    result text NOT NULL,
    rrset_summary jsonb NOT NULL DEFAULT '[]'::jsonb,
    matched_delegation_ids uuid[] NOT NULL DEFAULT '{}'::uuid[],
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    response_class text,
    response_code integer,
    request_digest bytea,
    response_digest bytea,
    deadline_at timestamptz,
    CONSTRAINT audit_events_request_id CHECK (octet_length(request_id) BETWEEN 1 AND 128),
    CONSTRAINT audit_events_action CHECK (action <> ''),
    CONSTRAINT audit_events_target CHECK (target_kind <> '' AND target_id <> ''),
    CONSTRAINT audit_events_rrset_summary CHECK (jsonb_typeof(rrset_summary) = 'array'),
    CONSTRAINT audit_events_details CHECK (jsonb_typeof(details) = 'object'),
    CONSTRAINT audit_events_response_code CHECK (
        response_code IS NULL OR response_code BETWEEN 100 AND 599
    ),
    CONSTRAINT audit_events_request_digest CHECK (
        request_digest IS NULL OR octet_length(request_digest) = 32
    ),
    CONSTRAINT audit_events_response_digest CHECK (
        response_digest IS NULL OR octet_length(response_digest) = 32
    ),
    CONSTRAINT audit_events_shape CHECK (
        (
            event_kind = 'management'
            AND operation_id IS NULL
            AND intent_event_id IS NULL
            AND intent_event_kind IS NULL
            AND result IN ('succeeded', 'failed')
            AND deadline_at IS NULL
        )
        OR (
            event_kind = 'authorization_denied'
            AND operation_id IS NULL
            AND intent_event_id IS NULL
            AND intent_event_kind IS NULL
            AND result = 'denied'
            AND deadline_at IS NULL
        )
        OR (
            event_kind = 'dns_intent'
            AND operation_id IS NOT NULL
            AND intent_event_id IS NULL
            AND intent_event_kind IS NULL
            AND result = 'pending'
            AND deadline_at IS NOT NULL
        )
        OR (
            event_kind = 'dns_outcome'
            AND operation_id IS NOT NULL
            AND intent_event_id IS NOT NULL
            AND intent_event_kind = 'dns_intent'
            AND result IN ('succeeded', 'failed', 'unknown')
            AND deadline_at IS NULL
        )
    ),
    UNIQUE (id, operation_id, event_kind),
    FOREIGN KEY (intent_event_id, operation_id, intent_event_kind)
        REFERENCES audit_events (id, operation_id, event_kind) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX audit_events_one_intent
    ON audit_events (operation_id)
    WHERE event_kind = 'dns_intent';

CREATE UNIQUE INDEX audit_events_one_outcome
    ON audit_events (operation_id)
    WHERE event_kind = 'dns_outcome';

CREATE UNIQUE INDEX audit_events_one_outcome_per_intent
    ON audit_events (intent_event_id)
    WHERE event_kind = 'dns_outcome';

CREATE INDEX audit_events_page
    ON audit_events (occurred_at DESC, id DESC);

CREATE INDEX audit_events_pending_intent_deadline
    ON audit_events (deadline_at, id)
    WHERE event_kind = 'dns_intent';
