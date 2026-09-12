-- name: CreateBrowserSession :one
INSERT INTO browser_sessions (digest, token_id, expires_at)
SELECT sqlc.arg('session_digest')::bytea, t.id,
       LEAST(statement_timestamp() + interval '168 hours', t.expires_at)
FROM api_tokens AS t
JOIN identities AS i ON i.id = t.identity_id
WHERE t.digest = sqlc.arg('token_digest')
  AND t.revoked_at IS NULL
  AND (t.expires_at IS NULL OR t.expires_at > statement_timestamp())
  AND i.enabled = true
RETURNING expires_at;

-- name: DeleteBrowserSession :exec
DELETE FROM browser_sessions WHERE digest = $1;

-- name: DeleteExpiredBrowserSessions :exec
DELETE FROM browser_sessions WHERE digest IN (
    SELECT digest FROM browser_sessions
    WHERE expires_at <= statement_timestamp()
    ORDER BY expires_at
    LIMIT 100
);
