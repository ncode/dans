-- name: TouchBrowseZone :one
INSERT INTO browse_zones(id, upstream, zone_id, cursor_key)
VALUES (@id::uuid, @upstream::text, @zone_id::text, @cursor_key::bytea)
ON CONFLICT (upstream, zone_id) DO UPDATE SET last_viewed_at = now()
RETURNING *;

-- name: GetBrowseZone :one
SELECT * FROM browse_zones WHERE upstream = @upstream::text AND zone_id = @zone_id::text;

-- name: RequestBrowseRefresh :exec
UPDATE browse_zones SET full_requested = true, retry_at = now()
WHERE id = @id::uuid AND full_lease IS NULL;

-- name: ListBrowseRRsets :many
SELECT name, type, payload FROM browse_rrsets
WHERE zone = @zone::uuid AND generation = @generation::uuid
AND name >= @name_from::text AND name <= @name_through::text
AND (name, type) > (@after_name::text, @after_type::text)
ORDER BY name, type LIMIT 101;

-- name: ListBrowseRRsetsByType :many
SELECT name, type, payload FROM browse_rrsets
WHERE zone = @zone::uuid AND generation = @generation::uuid AND type = @type::text
AND name >= @name_from::text AND name <= @name_through::text
AND (name, type) > (@after_name::text, @after_type::text)
ORDER BY name, type LIMIT 101;

-- name: BrowseHasPending :one
SELECT EXISTS(SELECT 1 FROM browse_changes WHERE zone = @zone::uuid AND NOT processed);
