CREATE TABLE browser_sessions (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    token_id uuid NOT NULL REFERENCES api_tokens (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '168 hours')
);
CREATE INDEX browser_sessions_expiry ON browser_sessions (expires_at);
