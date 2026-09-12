-- Rebuildable API observations. No foreign key to zone authority: unbound zones
-- remain readable, and every binding lifecycle change retires observations.
CREATE TABLE browse_zones (
    id uuid PRIMARY KEY,
    upstream text COLLATE "C" NOT NULL,
    zone_id text COLLATE "C" NOT NULL,
    generation uuid,
    revision bigint NOT NULL DEFAULT 0,
    cursor_key bytea NOT NULL CHECK (octet_length(cursor_key) = 32),
    last_viewed_at timestamptz NOT NULL DEFAULT now(),
    refreshed_at timestamptz,
    error text,
    full_requested boolean NOT NULL DEFAULT true,
    full_lease uuid,
    full_lease_until timestamptz,
    full_started_at timestamptz,
    retry_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (upstream, zone_id)
);
CREATE TABLE browse_rrsets (
    zone uuid NOT NULL REFERENCES browse_zones(id) ON DELETE CASCADE,
    generation uuid NOT NULL,
    name text COLLATE "C" NOT NULL,
    type text COLLATE "C" NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (zone, generation, name, type),
    CHECK (name = lower(name) AND right(name, 1) = '.'),
    CHECK (type ~ '^[A-Z][A-Z0-9-]{0,31}$')
);
CREATE INDEX browse_rrsets_type ON browse_rrsets(zone, generation, type, name);
CREATE TABLE browse_changes (
    zone uuid NOT NULL REFERENCES browse_zones(id) ON DELETE CASCADE,
    operation uuid NOT NULL,
    name text COLLATE "C" NOT NULL,
    type text COLLATE "C" NOT NULL,
    ready_at timestamptz NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now(),
    processed boolean NOT NULL DEFAULT false,
    lease uuid,
    lease_until timestamptz,
    PRIMARY KEY (zone, operation, name, type)
);
CREATE INDEX browse_changes_pending ON browse_changes(ready_at) WHERE NOT processed;

-- An intent queues reads at its deadline in case the process dies before an
-- outcome. Any outcome (including unknown closure) makes the read eligible now.
CREATE FUNCTION track_browse_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    observed browse_zones%ROWTYPE;
    intent audit_events%ROWTYPE;
    tuple jsonb;
BEGIN
    IF NEW.event_kind NOT IN ('dns_intent', 'dns_outcome') THEN RETURN NEW; END IF;
    IF NEW.event_kind = 'dns_outcome' THEN
        SELECT * INTO intent FROM audit_events WHERE id = NEW.intent_event_id;
    ELSE intent := NEW;
    END IF;
    IF intent.target_kind = 'zone_binding' THEN
        SELECT z.* INTO observed FROM browse_zones z JOIN zone_bindings b
          ON b.upstream = z.upstream AND b.powerdns_zone_id = z.zone_id
          WHERE b.id::text = intent.target_id;
    ELSIF intent.target_kind = 'powerdns_zone' THEN
        SELECT * INTO observed FROM browse_zones
          WHERE upstream = split_part(intent.target_id, ':', 1)
          AND zone_id = substring(intent.target_id FROM position(':' IN intent.target_id) + 1);
    ELSE RETURN NEW;
    END IF;
    IF observed.id IS NULL THEN RETURN NEW; END IF;
    IF intent.action IN ('powerdns.zone.delete', 'powerdns.zone.create') THEN
        DELETE FROM browse_zones WHERE id = observed.id;
        RETURN NEW;
    END IF;
    IF jsonb_array_length(intent.rrset_summary) > 0 THEN
        FOR tuple IN SELECT * FROM jsonb_array_elements(intent.rrset_summary) LOOP
            INSERT INTO browse_changes(zone, operation, name, type, ready_at)
            VALUES (observed.id, intent.operation_id, tuple->>'Owner', tuple->>'RecordType',
                CASE WHEN NEW.event_kind = 'dns_outcome' THEN now() ELSE intent.deadline_at END)
            ON CONFLICT (zone, operation, name, type) DO UPDATE SET
                ready_at = EXCLUDED.ready_at, changed_at = clock_timestamp(),
                processed = false, lease = NULL, lease_until = NULL;
        END LOOP;
    ELSE
        INSERT INTO browse_changes(zone, operation, name, type, ready_at)
        VALUES (observed.id, intent.operation_id, '', '',
            CASE WHEN NEW.event_kind = 'dns_outcome' THEN now() ELSE intent.deadline_at END)
        ON CONFLICT (zone, operation, name, type) DO UPDATE SET
            ready_at = EXCLUDED.ready_at, changed_at = clock_timestamp(),
            processed = false, lease = NULL, lease_until = NULL;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER browse_mutation AFTER INSERT ON audit_events
    FOR EACH ROW EXECUTE FUNCTION track_browse_mutation();

CREATE FUNCTION retire_browse_lifetime() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM browse_zones WHERE upstream = NEW.upstream AND zone_id = NEW.powerdns_zone_id;
    RETURN NEW;
END $$;
CREATE TRIGGER browse_binding_lifetime AFTER INSERT OR UPDATE OF retired_at ON zone_bindings
    FOR EACH ROW EXECUTE FUNCTION retire_browse_lifetime();
