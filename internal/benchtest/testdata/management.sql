-- Synthetic management capacity data, after migrations and one bootstrap identity.
INSERT INTO identities (id, kind, handle)
SELECT ('1' || lpad(n::text, 7, '0') || '-0000-4000-8000-000000000000')::uuid,
       CASE WHEN n % 2 = 0 THEN 'service' ELSE 'user' END,
       'capacity-identity-' || lpad(n::text, 4, '0')
FROM generate_series(1, 1000) AS n;

INSERT INTO groups (id, handle)
SELECT ('2' || lpad(n::text, 7, '0') || '-0000-4000-8000-000000000000')::uuid,
       'capacity-group-' || lpad(n::text, 3, '0')
FROM generate_series(1, 100) AS n;

INSERT INTO group_memberships (group_id, identity_id, added_by_identity_id)
SELECT '20000001-0000-4000-8000-000000000000', id,
       (SELECT id FROM identities ORDER BY created_at, id LIMIT 1)
FROM identities WHERE starts_with(handle, 'capacity-identity-');

INSERT INTO api_tokens (id, identity_id, label, digest)
SELECT ('3' || lpad(((i-1)*100+n)::text, 7, '0') || '-0000-4000-8000-000000000000')::uuid,
       ('1' || lpad(i::text, 7, '0') || '-0000-4000-8000-000000000000')::uuid,
       'capacity-token-' || n,
       decode(md5('synthetic-a:' || i || ':' || n) || md5('synthetic-b:' || i || ':' || n), 'hex')
FROM generate_series(1, 1000) AS i CROSS JOIN generate_series(1, 100) AS n;

INSERT INTO audit_events (id, event_kind, request_id, action, target_kind, target_id, result)
SELECT ('6' || lpad(n::text, 7, '0') || '-0000-4000-8000-000000000000')::uuid,
       'management', 'synthetic-capacity', 'synthetic.capacity', 'identity',
       '10000001-0000-4000-8000-000000000000', 'succeeded'
FROM generate_series(1, 100000) AS n;
