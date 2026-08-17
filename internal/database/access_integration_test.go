//go:build integration

package database

import (
	"errors"
	"testing"

	"github.com/ncode/dans/internal/identifier"
)

func TestStoreAuthenticate(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)

	const identityID = "10000000-0000-4000-8000-000000000101"
	insertIdentity(t, conn, identityID, "auth-user")
	secret, digest, err := identifier.NewToken()
	if err != nil {
		t.Fatalf("identifier.NewToken() error = %v", err)
	}
	if _, err := conn.Exec(t.Context(), `
		INSERT INTO api_tokens (id, identity_id, label, digest)
		VALUES ('30000000-0000-4000-8000-000000000101', $1, 'primary', $2)`, identityID, digest[:]); err != nil {
		t.Fatalf("insert API token: %v", err)
	}

	actor, err := store.Authenticate(t.Context(), secret)
	if err != nil {
		t.Fatalf("Authenticate(active token) error = %v", err)
	}
	if actor.IdentityID != identityID || actor.TokenID != "30000000-0000-4000-8000-000000000101" {
		t.Errorf("Authenticate(active token) = %+v", actor)
	}

	tests := []struct {
		name   string
		mutate string
		token  string
	}{
		{name: "malformed", token: "not-a-token"},
		{name: "unknown", token: "dans_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{name: "revoked", mutate: "UPDATE api_tokens SET revoked_at = now() WHERE id = '30000000-0000-4000-8000-000000000101'", token: secret},
		{name: "disabled owner", mutate: "UPDATE api_tokens SET revoked_at = NULL; UPDATE identities SET enabled = false WHERE id = '10000000-0000-4000-8000-000000000101'", token: secret},
		{name: "expired", mutate: "UPDATE identities SET enabled = true; UPDATE api_tokens SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour'", token: secret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mutate != "" {
				if _, err := conn.Exec(t.Context(), tt.mutate); err != nil {
					t.Fatalf("prepare %s credential: %v", tt.name, err)
				}
			}
			if _, err := store.Authenticate(t.Context(), tt.token); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Authenticate(%s token) error = %v, want %v", tt.name, err, ErrUnauthenticated)
			}
		})
	}
}
