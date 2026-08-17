-- name: GetInstallationMetadata :one
SELECT installation_id, initialized_at, restore_required, restore_finalized_at
FROM installation_metadata
WHERE singleton = true;

-- name: AuthenticateToken :one
SELECT t.id AS token_id, i.id AS identity_id, i.kind, i.handle, i.is_operator
FROM api_tokens AS t
JOIN identities AS i ON i.id = t.identity_id
WHERE t.digest = $1
  AND t.revoked_at IS NULL
  AND (t.expires_at IS NULL OR t.expires_at > statement_timestamp())
  AND i.enabled = true;

-- name: GetIdentityByID :one
SELECT id, kind, handle, display_name, enabled, is_operator, created_at, updated_at
FROM identities
WHERE id = $1;

-- name: CreateIdentity :one
INSERT INTO identities (id, kind, handle, display_name)
VALUES ($1, $2, $3, $4)
RETURNING id, kind, handle, display_name, enabled, is_operator, created_at, updated_at;

-- name: ListIdentities :many
SELECT id, kind, handle, display_name, enabled, is_operator, created_at, updated_at
FROM identities
WHERE (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind'))
  AND (sqlc.narg('enabled')::boolean IS NULL OR enabled = sqlc.narg('enabled'))
  AND (sqlc.narg('is_operator')::boolean IS NULL OR is_operator = sqlc.narg('is_operator'))
  AND (sqlc.narg('handle')::text IS NULL OR handle = sqlc.narg('handle'))
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (created_at, id) < (
          sqlc.narg('after_created_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: LockOperatorGuard :one
SELECT singleton
FROM operator_guard
WHERE singleton = true
FOR UPDATE;

-- name: GetIdentityForUpdate :one
SELECT id, kind, handle, display_name, enabled, is_operator, created_at, updated_at
FROM identities
WHERE id = $1
FOR UPDATE;

-- name: CountEnabledOperators :one
SELECT count(*)
FROM identities
WHERE enabled = true AND is_operator = true;

-- name: UpdateIdentity :one
UPDATE identities
SET display_name = $2,
    enabled = $3,
    is_operator = $4,
    updated_at = statement_timestamp()
WHERE id = $1
RETURNING id, kind, handle, display_name, enabled, is_operator, created_at, updated_at;

-- name: InsertManagementAudit :one
INSERT INTO audit_events (
    id, event_kind, request_id, actor_identity_id, actor_token_id,
    action, target_kind, target_id, result, details
)
VALUES (
    sqlc.arg('id'), 'management', sqlc.arg('request_id'),
    NULLIF(sqlc.arg('actor_identity_id')::text, '')::uuid,
    NULLIF(sqlc.arg('actor_token_id')::text, '')::uuid,
    sqlc.arg('action'), sqlc.arg('target_kind'), sqlc.arg('target_id'),
    sqlc.arg('result'), sqlc.arg('details')
)
RETURNING id, occurred_at, event_kind, operation_id, intent_event_id,
          request_id, actor_identity_id, actor_token_id, action, target_kind,
          target_id, result, rrset_summary, matched_delegation_ids, details,
          response_class, response_code, request_digest, response_digest, deadline_at;

-- name: InsertAuthorizationDeniedAudit :one
INSERT INTO audit_events (
    id, event_kind, request_id, actor_identity_id, actor_token_id,
    action, target_kind, target_id, result, details, request_digest
)
VALUES (
    sqlc.arg('id'), 'authorization_denied', sqlc.arg('request_id'),
    NULLIF(sqlc.arg('actor_identity_id')::text, '')::uuid,
    NULLIF(sqlc.arg('actor_token_id')::text, '')::uuid,
    sqlc.arg('action'), sqlc.arg('target_kind'), sqlc.arg('target_id'),
    'denied', sqlc.arg('details'), sqlc.arg('request_digest')
)
RETURNING id, occurred_at, event_kind, operation_id, intent_event_id,
          request_id, actor_identity_id, actor_token_id, action, target_kind,
          target_id, result, rrset_summary, matched_delegation_ids, details,
          response_class, response_code, request_digest, response_digest, deadline_at;

-- name: InsertDNSIntent :one
INSERT INTO audit_events (
    id, event_kind, operation_id, request_id, actor_identity_id, actor_token_id,
    action, target_kind, target_id, result, rrset_summary,
    matched_delegation_ids, details, request_digest, deadline_at
)
VALUES (
    sqlc.arg('id'), 'dns_intent', NULLIF(sqlc.arg('operation_id')::text, '')::uuid,
    sqlc.arg('request_id'),
    NULLIF(sqlc.arg('actor_identity_id')::text, '')::uuid,
    NULLIF(sqlc.arg('actor_token_id')::text, '')::uuid,
    sqlc.arg('action'), sqlc.arg('target_kind'), sqlc.arg('target_id'),
    'pending', sqlc.arg('rrset_summary'),
    COALESCE(sqlc.arg('matched_delegation_ids')::uuid[], '{}'::uuid[]),
    '{}'::jsonb, sqlc.arg('request_digest'), sqlc.arg('deadline_at')
)
RETURNING id, occurred_at, event_kind, operation_id, intent_event_id,
          request_id, actor_identity_id, actor_token_id, action, target_kind,
          target_id, result, rrset_summary, matched_delegation_ids, details,
          response_class, response_code, request_digest, response_digest, deadline_at;

-- name: InsertDNSOutcome :execrows
INSERT INTO audit_events (
    id, event_kind, operation_id, intent_event_id, intent_event_kind,
    request_id, actor_identity_id, actor_token_id, action, target_kind,
    target_id, result, rrset_summary, matched_delegation_ids, details,
    response_class, response_code, request_digest, response_digest
)
SELECT sqlc.arg('id'), 'dns_outcome', intent.operation_id, intent.id, 'dns_intent',
       intent.request_id, intent.actor_identity_id, intent.actor_token_id,
       intent.action, intent.target_kind, intent.target_id, sqlc.arg('result'),
       intent.rrset_summary, intent.matched_delegation_ids, '{}'::jsonb,
       NULLIF(sqlc.arg('response_class')::text, ''), sqlc.narg('response_code')::integer,
       intent.request_digest, NULLIF(sqlc.arg('response_digest')::bytea, ''::bytea)
FROM audit_events AS intent
WHERE intent.id = NULLIF(sqlc.arg('intent_event_id')::text, '')::uuid
  AND intent.operation_id = NULLIF(sqlc.arg('operation_id')::text, '')::uuid
  AND intent.event_kind = 'dns_intent'
ON CONFLICT (operation_id) WHERE event_kind = 'dns_outcome' DO NOTHING;

-- name: GetDNSOutcomeByOperationID :one
SELECT id, event_kind, operation_id::text AS operation_id,
       intent_event_id::text AS intent_event_id, result,
       response_class, response_code, response_digest, occurred_at
FROM audit_events
WHERE operation_id = NULLIF(sqlc.arg('operation_id')::text, '')::uuid
  AND event_kind = 'dns_outcome';

-- name: CloseOverdueDNSIntents :many
WITH candidates AS (
    SELECT intent.id
    FROM audit_events AS intent
    WHERE intent.event_kind = 'dns_intent'
      AND intent.deadline_at <= statement_timestamp()
      AND cardinality(sqlc.arg('event_ids')::uuid[]) = sqlc.arg('row_limit')
      AND NOT EXISTS (
          SELECT 1
          FROM audit_events AS outcome
          WHERE outcome.operation_id = intent.operation_id
            AND outcome.event_kind = 'dns_outcome'
      )
    ORDER BY intent.deadline_at, intent.id
    LIMIT sqlc.arg('row_limit')
    FOR UPDATE SKIP LOCKED
),
numbered AS (
    SELECT intent.*, row_number() OVER (ORDER BY intent.deadline_at, intent.id) AS position
    FROM audit_events AS intent
    JOIN candidates ON candidates.id = intent.id
),
event_ids AS (
    SELECT value AS id, ordinality AS position
    FROM unnest(sqlc.arg('event_ids')::uuid[]) WITH ORDINALITY AS generated(value, ordinality)
)
INSERT INTO audit_events (
    id, event_kind, operation_id, intent_event_id, intent_event_kind,
    request_id, actor_identity_id, actor_token_id, action, target_kind,
    target_id, result, rrset_summary, matched_delegation_ids, details,
    response_class, request_digest
)
SELECT event_ids.id, 'dns_outcome', intent.operation_id, intent.id, 'dns_intent',
       intent.request_id, intent.actor_identity_id, intent.actor_token_id,
       intent.action, intent.target_kind, intent.target_id, 'unknown',
       intent.rrset_summary, intent.matched_delegation_ids,
       '{"closed_by":"deadline"}'::jsonb, 'deadline_elapsed', intent.request_digest
FROM numbered AS intent
JOIN event_ids ON event_ids.position = intent.position
ON CONFLICT (operation_id) WHERE event_kind = 'dns_outcome' DO NOTHING
RETURNING id;

-- name: ListAuditRecords :many
SELECT id, occurred_at, request_id,
       COALESCE(actor_identity_id::text, '')::text AS actor_id,
       action, target_kind, target_id, result, details
FROM audit_events
WHERE (NULLIF(sqlc.arg('actor_id')::text, '') IS NULL
       OR actor_identity_id = NULLIF(sqlc.arg('actor_id')::text, '')::uuid)
  AND (NULLIF(sqlc.arg('action')::text, '') IS NULL OR action = sqlc.arg('action'))
  AND (NULLIF(sqlc.arg('target_kind')::text, '') IS NULL OR target_kind = sqlc.arg('target_kind'))
  AND (NULLIF(sqlc.arg('target_id')::text, '') IS NULL OR target_id = sqlc.arg('target_id'))
  AND (NULLIF(sqlc.arg('result')::text, '') IS NULL OR result = sqlc.arg('result'))
  AND (
      sqlc.narg('after_occurred_at')::timestamptz IS NULL
      OR (occurred_at, id) < (
          sqlc.narg('after_occurred_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY occurred_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: GetGroupByID :one
SELECT id, handle, display_name, enabled, created_at, updated_at
FROM groups
WHERE id = $1;

-- name: CreateGroup :one
INSERT INTO groups (id, handle, display_name)
VALUES ($1, $2, $3)
RETURNING id, handle, display_name, enabled, created_at, updated_at;

-- name: ListGroups :many
SELECT id, handle, display_name, enabled, created_at, updated_at
FROM groups
WHERE (sqlc.narg('enabled')::boolean IS NULL OR enabled = sqlc.narg('enabled'))
  AND (sqlc.narg('handle')::text IS NULL OR handle = sqlc.narg('handle'))
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (created_at, id) < (
          sqlc.narg('after_created_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: GetGroupForUpdate :one
SELECT id, handle, display_name, enabled, created_at, updated_at
FROM groups
WHERE id = $1
FOR UPDATE;

-- name: UpdateGroup :one
UPDATE groups
SET display_name = $2,
    enabled = $3,
    updated_at = statement_timestamp()
WHERE id = $1
RETURNING id, handle, display_name, enabled, created_at, updated_at;

-- name: AddGroupMembership :execrows
INSERT INTO group_memberships (group_id, identity_id, added_by_identity_id)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, identity_id) DO NOTHING;

-- name: GetGroupMembership :one
SELECT group_id, identity_id, added_by_identity_id, created_at
FROM group_memberships
WHERE group_id = $1 AND identity_id = $2;

-- name: RemoveGroupMembership :execrows
DELETE FROM group_memberships
WHERE group_id = $1 AND identity_id = $2;

-- name: ListGroupMembers :many
SELECT i.id, i.kind, i.handle, i.display_name, i.enabled, i.is_operator,
       i.created_at, i.updated_at
FROM group_memberships AS m
JOIN identities AS i ON i.id = m.identity_id
WHERE m.group_id = sqlc.arg('group_id')
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (i.created_at, i.id) < (
          sqlc.narg('after_created_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY i.created_at DESC, i.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListIdentityGroups :many
SELECT g.id, g.handle, g.display_name, g.enabled, g.created_at, g.updated_at
FROM group_memberships AS m
JOIN groups AS g ON g.id = m.group_id
WHERE m.identity_id = sqlc.arg('identity_id')
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (g.created_at, g.id) < (
          sqlc.narg('after_created_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY g.created_at DESC, g.id DESC
LIMIT sqlc.arg('row_limit');

-- name: CreateAPIToken :one
INSERT INTO api_tokens (id, identity_id, label, digest, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, identity_id, label, expires_at, revoked_at, created_at,
          CASE
              WHEN revoked_at IS NOT NULL THEN 'revoked'
              WHEN expires_at IS NOT NULL AND expires_at <= statement_timestamp() THEN 'expired'
              ELSE 'active'
          END::text AS status;

-- name: ListAPITokens :many
SELECT id, identity_id, label, expires_at, revoked_at, created_at,
       CASE
           WHEN revoked_at IS NOT NULL THEN 'revoked'
           WHEN expires_at IS NOT NULL AND expires_at <= statement_timestamp() THEN 'expired'
           ELSE 'active'
       END::text AS status
FROM api_tokens
WHERE identity_id = sqlc.arg('identity_id')
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (created_at, id) < (
          sqlc.narg('after_created_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: GetAPITokenMetadata :one
SELECT id, identity_id, label, expires_at, revoked_at, created_at,
       CASE
           WHEN revoked_at IS NOT NULL THEN 'revoked'
           WHEN expires_at IS NOT NULL AND expires_at <= statement_timestamp() THEN 'expired'
           ELSE 'active'
       END::text AS status
FROM api_tokens
WHERE identity_id = $1 AND id = $2;

-- name: RevokeAPIToken :one
UPDATE api_tokens
SET revoked_at = statement_timestamp()
WHERE identity_id = $1 AND id = $2 AND revoked_at IS NULL
RETURNING id, identity_id, label, expires_at, revoked_at, created_at,
          'revoked'::text AS status;

-- name: GetBootstrapState :one
SELECT
    (SELECT count(*) FROM installation_metadata) AS installations,
    (SELECT count(*) FROM identities) AS identities;

-- name: CreateInstallationMetadata :one
INSERT INTO installation_metadata (installation_id)
VALUES ($1)
RETURNING singleton, installation_id, initialized_at, restore_required, restore_finalized_at;

-- name: CreateOperatorIdentity :one
INSERT INTO identities (id, kind, handle, display_name, is_operator)
VALUES ($1, 'user', $2, $3, true)
RETURNING id, kind, handle, display_name, enabled, is_operator, created_at, updated_at;

-- name: GetIdentityByHandleForUpdate :one
SELECT id, kind, handle, display_name, enabled, is_operator, created_at, updated_at
FROM identities
WHERE handle = $1
FOR UPDATE;

-- name: RevokeAllActiveAPITokens :execrows
UPDATE api_tokens
SET revoked_at = statement_timestamp()
WHERE revoked_at IS NULL;

-- name: FinalizeInstallationRestore :one
UPDATE installation_metadata
SET restore_required = false,
    restore_finalized_at = statement_timestamp()
WHERE singleton = true
RETURNING singleton, installation_id, initialized_at, restore_required, restore_finalized_at;

-- name: GetZoneBindingByID :one
SELECT id, generation, upstream, powerdns_zone_id, zone_name,
       created_by_identity_id, retired_by_identity_id, created_at, retired_at
FROM zone_bindings
WHERE id = $1;

-- name: GetZoneBindingForUpdate :one
SELECT id, generation, upstream, powerdns_zone_id, zone_name,
       created_by_identity_id, retired_by_identity_id, created_at, retired_at
FROM zone_bindings
WHERE id = $1
FOR UPDATE;

-- name: GetActiveZoneBindingByUpstreamZoneIDForUpdate :one
SELECT id, generation, upstream, powerdns_zone_id, zone_name,
       created_by_identity_id, retired_by_identity_id, created_at, retired_at
FROM zone_bindings
WHERE upstream = $1
  AND powerdns_zone_id = $2
  AND retired_at IS NULL
FOR UPDATE;

-- name: LockZoneBindingKey :exec
SELECT pg_advisory_xact_lock(1145130580, hashtext($1));

-- name: ListActiveZoneBindingCandidates :many
SELECT id, generation, upstream, powerdns_zone_id, zone_name,
       created_by_identity_id, retired_by_identity_id, created_at, retired_at
FROM zone_bindings
WHERE upstream = sqlc.arg('upstream')
  AND retired_at IS NULL
  AND (powerdns_zone_id = sqlc.arg('powerdns_zone_id') OR zone_name = sqlc.arg('zone_name'))
ORDER BY id;

-- name: ZoneBindingHistoryExists :one
SELECT EXISTS (
    SELECT 1
    FROM zone_bindings
    WHERE upstream = sqlc.arg('upstream')
      AND (powerdns_zone_id = sqlc.arg('powerdns_zone_id') OR zone_name = sqlc.arg('zone_name'))
);

-- name: NextZoneBindingGeneration :one
SELECT (COALESCE(max(generation), 0) + 1)::bigint
FROM zone_bindings
WHERE upstream = $1 AND zone_name = $2;

-- name: CreateZoneBinding :one
INSERT INTO zone_bindings (
    id, generation, upstream, powerdns_zone_id, zone_name, created_by_identity_id
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, generation, upstream, powerdns_zone_id, zone_name,
          created_by_identity_id, retired_by_identity_id, created_at, retired_at;

-- name: ListZoneBindings :many
SELECT id, generation, upstream, powerdns_zone_id, zone_name,
       created_by_identity_id, retired_by_identity_id, created_at, retired_at
FROM zone_bindings
WHERE (NULLIF(sqlc.arg('status')::text, '') IS NULL
       OR sqlc.arg('status') = 'active' AND retired_at IS NULL
       OR sqlc.arg('status') = 'retired' AND retired_at IS NOT NULL)
  AND (NULLIF(sqlc.arg('zone_name')::text, '') IS NULL OR zone_name = sqlc.arg('zone_name'))
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (created_at, id) < (
          sqlc.narg('after_created_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: RevokeDelegationsForZoneDeletion :many
UPDATE delegations
SET revoked_at = statement_timestamp(),
    revoked_by_identity_id = NULLIF(sqlc.arg('revoked_by_identity_id')::text, '')::uuid
WHERE zone_binding_id = sqlc.arg('zone_binding_id')
  AND revoked_at IS NULL
RETURNING id;

-- name: RetireZoneBinding :one
UPDATE zone_bindings
SET retired_at = statement_timestamp(),
    retired_by_identity_id = NULLIF(sqlc.arg('retired_by_identity_id')::text, '')::uuid
WHERE id = sqlc.arg('id') AND retired_at IS NULL
RETURNING id, generation, upstream, powerdns_zone_id, zone_name,
          created_by_identity_id, retired_by_identity_id, created_at, retired_at;

-- name: GetZoneDeletionReconciliationState :one
SELECT
    EXISTS (
        SELECT 1
        FROM audit_events AS intent
        WHERE intent.event_kind = 'dns_intent'
          AND intent.action = 'powerdns.zone.delete'
          AND intent.target_kind = 'zone_binding'
          AND intent.target_id = sqlc.arg('binding_id')
    ) AS has_attempt,
    EXISTS (
        SELECT 1
        FROM audit_events AS intent
        LEFT JOIN audit_events AS outcome
          ON outcome.intent_event_id = intent.id
         AND outcome.event_kind = 'dns_outcome'
        WHERE intent.event_kind = 'dns_intent'
          AND intent.action = 'powerdns.zone.delete'
          AND intent.target_kind = 'zone_binding'
          AND intent.target_id = sqlc.arg('binding_id')
          AND outcome.id IS NULL
    ) AS has_pending,
    EXISTS (
        SELECT 1
        FROM audit_events AS intent
        JOIN audit_events AS outcome
          ON outcome.intent_event_id = intent.id
         AND outcome.event_kind = 'dns_outcome'
        WHERE intent.event_kind = 'dns_intent'
          AND intent.action = 'powerdns.zone.delete'
          AND intent.target_kind = 'zone_binding'
          AND intent.target_id = sqlc.arg('binding_id')
          AND outcome.result = 'succeeded'
    ) AS has_succeeded,
    EXISTS (
        SELECT 1
        FROM audit_events AS intent
        JOIN audit_events AS outcome
          ON outcome.intent_event_id = intent.id
         AND outcome.event_kind = 'dns_outcome'
        WHERE intent.event_kind = 'dns_intent'
          AND intent.action = 'powerdns.zone.delete'
          AND intent.target_kind = 'zone_binding'
          AND intent.target_id = sqlc.arg('binding_id')
          AND outcome.result IN ('failed', 'unknown')
    ) AS has_retryable,
    EXISTS (
        SELECT 1
        FROM audit_events AS event
        WHERE event.event_kind = 'management'
          AND event.action = 'zone_binding.confirm_absent'
          AND event.target_kind = 'zone_binding'
          AND event.target_id = sqlc.arg('binding_id')
          AND event.result = 'succeeded'
    ) AS confirmed_absent;

-- name: GetDelegationByID :one
SELECT id, zone_binding_id, grantee_identity_id, grantee_group_id,
       created_by_identity_id, revoked_by_identity_id, created_at, revoked_at
FROM delegations
WHERE id = $1;

-- name: CreateDelegation :one
INSERT INTO delegations (
    id, zone_binding_id, grantee_identity_id, grantee_group_id, created_by_identity_id
)
VALUES (
    sqlc.arg('id'), sqlc.arg('zone_binding_id'),
    NULLIF(sqlc.arg('grantee_identity_id')::text, '')::uuid,
    NULLIF(sqlc.arg('grantee_group_id')::text, '')::uuid,
    sqlc.arg('created_by_identity_id')
)
RETURNING id, zone_binding_id, grantee_identity_id, grantee_group_id,
          created_by_identity_id, revoked_by_identity_id, created_at, revoked_at;

-- name: CreateDelegationSelector :exec
INSERT INTO delegation_selectors (
    delegation_id, position, kind, selector, sql_like_pattern
)
VALUES ($1, $2, $3, $4, $5);

-- name: CreateDelegationRecordType :exec
INSERT INTO delegation_record_types (delegation_id, record_type)
VALUES ($1, $2);

-- name: CreateDelegationChangeKind :exec
INSERT INTO delegation_change_kinds (delegation_id, change_kind)
VALUES ($1, $2);

-- name: GetDelegationDetails :one
SELECT id, zone_binding_id,
       CASE WHEN grantee_identity_id IS NOT NULL THEN 'identity' ELSE 'group' END::text AS grantee_kind,
       COALESCE(grantee_identity_id, grantee_group_id)::text AS grantee_id,
       created_at, revoked_at
FROM delegations
WHERE id = $1;

-- name: ListDelegationDetails :many
SELECT id, zone_binding_id,
       CASE WHEN grantee_identity_id IS NOT NULL THEN 'identity' ELSE 'group' END::text AS grantee_kind,
       COALESCE(grantee_identity_id, grantee_group_id)::text AS grantee_id,
       created_at, revoked_at
FROM delegations
WHERE (NULLIF(sqlc.arg('zone_binding_id')::text, '') IS NULL
       OR zone_binding_id = NULLIF(sqlc.arg('zone_binding_id')::text, '')::uuid)
  AND (NULLIF(sqlc.arg('grantee_id')::text, '') IS NULL
       OR grantee_identity_id = NULLIF(sqlc.arg('grantee_id')::text, '')::uuid
       OR grantee_group_id = NULLIF(sqlc.arg('grantee_id')::text, '')::uuid)
  AND (sqlc.narg('active')::boolean IS NULL
       OR (revoked_at IS NULL) = sqlc.narg('active'))
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (created_at, id) < (
          sqlc.narg('after_created_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListEffectiveDelegationDetails :many
SELECT d.id, d.zone_binding_id,
       CASE WHEN d.grantee_identity_id IS NOT NULL THEN 'identity' ELSE 'group' END::text AS grantee_kind,
       COALESCE(d.grantee_identity_id, d.grantee_group_id)::text AS grantee_id,
       d.created_at, d.revoked_at
FROM delegations AS d
JOIN zone_bindings AS binding
  ON binding.id = d.zone_binding_id
 AND binding.retired_at IS NULL
WHERE d.revoked_at IS NULL
  AND (
      d.grantee_identity_id = NULLIF(sqlc.arg('identity_id')::text, '')::uuid
      OR EXISTS (
          SELECT 1
          FROM group_memberships AS membership
          JOIN groups AS member_group
            ON member_group.id = membership.group_id
           AND member_group.enabled = true
          WHERE membership.identity_id = NULLIF(sqlc.arg('identity_id')::text, '')::uuid
            AND membership.group_id = d.grantee_group_id
      )
  )
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (d.created_at, d.id) < (
          sqlc.narg('after_created_at'),
          NULLIF(sqlc.arg('after_id')::text, '')::uuid
      )
  )
ORDER BY d.created_at DESC, d.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListDelegationSelectorsByIDs :many
SELECT delegation_id, position, kind, selector, sql_like_pattern
FROM delegation_selectors
WHERE delegation_id = ANY(sqlc.arg('delegation_ids')::uuid[])
ORDER BY delegation_id, position;

-- name: ListDelegationRecordTypesByIDs :many
SELECT delegation_id, record_type
FROM delegation_record_types
WHERE delegation_id = ANY(sqlc.arg('delegation_ids')::uuid[])
ORDER BY delegation_id, record_type;

-- name: ListDelegationChangeKindsByIDs :many
SELECT delegation_id, change_kind
FROM delegation_change_kinds
WHERE delegation_id = ANY(sqlc.arg('delegation_ids')::uuid[])
ORDER BY delegation_id, change_kind;

-- name: GetDelegationForUpdate :one
SELECT id, zone_binding_id, grantee_identity_id, grantee_group_id,
       created_by_identity_id, revoked_by_identity_id, created_at, revoked_at
FROM delegations
WHERE id = $1
FOR UPDATE;

-- name: RevokeDelegation :one
UPDATE delegations
SET revoked_at = statement_timestamp(),
    revoked_by_identity_id = NULLIF(sqlc.arg('revoked_by_identity_id')::text, '')::uuid
WHERE id = $1 AND revoked_at IS NULL
RETURNING id, zone_binding_id, grantee_identity_id, grantee_group_id,
          created_by_identity_id, revoked_by_identity_id, created_at, revoked_at;

-- name: AuthorizeRRsetBatch :many
WITH authenticated AS (
    SELECT t.id AS token_id, i.id AS identity_id, i.kind, i.handle, i.is_operator
    FROM api_tokens AS t
    JOIN identities AS i ON i.id = t.identity_id
    WHERE t.digest = sqlc.arg('token_digest')
      AND t.revoked_at IS NULL
      AND (t.expires_at IS NULL OR t.expires_at > statement_timestamp())
      AND i.enabled = true
),
input_tuples AS (
    SELECT (item.ordinality - 1)::integer AS batch_index,
           item.value ->> 'owner' AS owner,
           item.value ->> 'record_type' AS record_type,
           item.value ->> 'change_kind' AS change_kind,
           (item.value ->> 'literal_wildcard')::boolean AS literal_wildcard
    FROM jsonb_array_elements(sqlc.arg('tuples')::jsonb) WITH ORDINALITY AS item(value, ordinality)
),
active_binding AS (
    SELECT id, zone_name
    FROM zone_bindings
    WHERE upstream = sqlc.arg('upstream')
      AND powerdns_zone_id = sqlc.arg('powerdns_zone_id')
      AND retired_at IS NULL
)
SELECT COALESCE(a.token_id::text, '')::text AS token_id,
       COALESCE(a.identity_id::text, '')::text AS identity_id,
       COALESCE(a.kind, '') AS identity_kind,
       COALESCE(a.handle, '') AS identity_handle,
       COALESCE(a.is_operator, false) AS is_operator,
       COALESCE(b.id::text, '')::text AS zone_binding_id,
       t.batch_index, t.owner::text AS owner, t.record_type::text AS record_type,
       t.change_kind::text AS change_kind,
       (COALESCE(a.is_operator, false) OR count(DISTINCT d.id) > 0)::boolean AS allowed,
       COALESCE(
           array_agg(DISTINCT d.id) FILTER (WHERE d.id IS NOT NULL),
           '{}'::uuid[]
       )::uuid[] AS matched_delegation_ids
FROM input_tuples AS t
LEFT JOIN authenticated AS a ON true
LEFT JOIN active_binding AS b ON true
LEFT JOIN delegations AS d
  ON d.zone_binding_id = b.id
 AND d.revoked_at IS NULL
 AND (
      d.grantee_identity_id = a.identity_id
      OR (
          d.grantee_group_id IS NOT NULL
          AND EXISTS (
              SELECT 1
              FROM group_memberships AS membership
              JOIN groups AS member_group
                ON member_group.id = membership.group_id
               AND member_group.enabled = true
              WHERE membership.identity_id = a.identity_id
                AND membership.group_id = d.grantee_group_id
          )
      )
 )
 AND (
      b.zone_name = '.'
      OR t.owner = b.zone_name
      OR right(t.owner, length(b.zone_name) + 1) = '.' || b.zone_name
 )
 AND EXISTS (
      SELECT 1
      FROM delegation_selectors AS selector
      WHERE selector.delegation_id = d.id
        AND (
            (selector.kind = 'exact' AND selector.selector = t.owner)
            OR (
                selector.kind = 'glob'
                AND t.owner <> b.zone_name
                AND NOT t.literal_wildcard
                AND t.owner COLLATE "C" LIKE selector.sql_like_pattern COLLATE "C" ESCAPE '\'
            )
        )
 )
 AND (
      NOT EXISTS (
          SELECT 1 FROM delegation_record_types AS record_filter
          WHERE record_filter.delegation_id = d.id
      )
      OR EXISTS (
          SELECT 1 FROM delegation_record_types AS record_filter
          WHERE record_filter.delegation_id = d.id
            AND record_filter.record_type = t.record_type
      )
 )
 AND (
      NOT EXISTS (
          SELECT 1 FROM delegation_change_kinds AS change_filter
          WHERE change_filter.delegation_id = d.id
      )
      OR EXISTS (
          SELECT 1 FROM delegation_change_kinds AS change_filter
          WHERE change_filter.delegation_id = d.id
            AND change_filter.change_kind = t.change_kind
      )
 )
GROUP BY a.token_id, a.identity_id, a.kind, a.handle, a.is_operator,
         b.id, t.batch_index, t.owner, t.record_type, t.change_kind
ORDER BY t.batch_index;

-- name: GetAuditEventByID :one
SELECT id, occurred_at, event_kind, operation_id, intent_event_id,
       request_id, actor_identity_id, actor_token_id, action, target_kind,
       target_id, result, rrset_summary, matched_delegation_ids, details,
       response_class, response_code, request_digest, response_digest, deadline_at
FROM audit_events
WHERE id = $1;
