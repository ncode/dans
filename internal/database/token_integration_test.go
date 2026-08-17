//go:build integration

package database

import (
	"errors"
	"testing"
	"time"

	"github.com/ncode/dans/internal/identifier"
)

func TestStoreTokenLifecycleAndVisibility(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "204")
	ownerIdentity := insertTestIdentity(t, conn, "205", "owner-205")
	owner := insertTestActorToken(t, conn, ownerIdentity, "305")
	other := insertTestIdentity(t, conn, "206", "other-206")

	expiresAt := time.Now().Add(time.Hour).UTC()
	created, err := store.CreateToken(t.Context(), operator, ownerIdentity.ID, TokenCreate{
		Label:     "automation",
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateToken(operator) error = %v", err)
	}
	if identifier.ValidateToken(created.Secret) != nil || created.Token.ID == "" || created.Token.Status != "active" {
		t.Errorf("CreateToken(operator) = %+v", created)
	}
	if created.Token.IdentityID != ownerIdentity.ID || created.Token.Label != "automation" {
		t.Errorf("created token metadata = %+v", created.Token)
	}
	var storedSecret bool
	if err := conn.QueryRow(t.Context(), `SELECT digest = convert_to($2, 'UTF8') FROM api_tokens WHERE id = $1`, created.Token.ID, created.Secret).Scan(&storedSecret); err != nil {
		t.Fatalf("compare stored digest: %v", err)
	}
	if storedSecret {
		t.Error("token plaintext was stored as digest bytes")
	}
	assertAuditCount(t, conn, "token.create", created.Token.ID, 1)

	if _, err := store.CreateToken(t.Context(), operator, ownerIdentity.ID, TokenCreate{Label: "automation"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateToken(duplicate active label) error = %v, want %v", err, ErrConflict)
	}
	past := time.Now().Add(-time.Minute)
	if _, err := store.CreateToken(t.Context(), owner, ownerIdentity.ID, TokenCreate{Label: "past", ExpiresAt: &past}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("CreateToken(past expiry) error = %v, want %v", err, ErrInvalid)
	}
	if _, err := store.CreateToken(t.Context(), owner, other.ID, TokenCreate{Label: "forbidden"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("CreateToken(other identity) error = %v, want %v", err, ErrForbidden)
	}

	selfCreated, err := store.CreateToken(t.Context(), owner, ownerIdentity.ID, TokenCreate{Label: "self"})
	if err != nil {
		t.Fatalf("CreateToken(self) error = %v", err)
	}
	page, err := store.ListTokens(t.Context(), owner, ownerIdentity.ID, TokenListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListTokens(self) error = %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("ListTokens(self) count = %d, want 3", len(page.Items))
	}
	if _, err := store.ListTokens(t.Context(), owner, other.ID, TokenListOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListTokens(other identity) error = %v, want %v", err, ErrForbidden)
	}

	if err := store.RevokeToken(t.Context(), owner, ownerIdentity.ID, selfCreated.Token.ID); err != nil {
		t.Fatalf("RevokeToken(self) error = %v", err)
	}
	if err := store.RevokeToken(t.Context(), owner, ownerIdentity.ID, selfCreated.Token.ID); err != nil {
		t.Fatalf("RevokeToken(self repeat) error = %v", err)
	}
	if _, err := store.Authenticate(t.Context(), selfCreated.Secret); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate(revoked secret) error = %v, want %v", err, ErrUnauthenticated)
	}
	assertAuditCount(t, conn, "token.revoke", selfCreated.Token.ID, 1)
	page, err = store.ListTokens(t.Context(), operator, ownerIdentity.ID, TokenListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListTokens(operator) error = %v", err)
	}
	if statusForToken(page.Items, selfCreated.Token.ID) != "revoked" {
		t.Errorf("revoked token status = %q, want revoked", statusForToken(page.Items, selfCreated.Token.ID))
	}
	if _, err := store.CreateToken(t.Context(), owner, ownerIdentity.ID, TokenCreate{Label: "self"}); err != nil {
		t.Fatalf("CreateToken(reused revoked label) error = %v", err)
	}
}

func insertTestActorToken(t *testing.T, conn DBTX, identity Identity, suffix string) Actor {
	t.Helper()
	actor, _ := insertTestActorTokenWithSecret(t, conn, identity, suffix)
	return actor
}

func insertTestActorTokenWithSecret(t *testing.T, conn DBTX, identity Identity, suffix string) (Actor, string) {
	t.Helper()
	secret, digest, err := identifier.NewToken()
	if err != nil {
		t.Fatalf("identifier.NewToken() error = %v", err)
	}
	tokenID := "30000000-0000-4000-8000-000000000" + suffix
	if _, err := conn.Exec(t.Context(), `
		INSERT INTO api_tokens (id, identity_id, label, digest)
		VALUES ($1, $2, 'session', $3)`, tokenID, identity.ID, digest[:]); err != nil {
		t.Fatalf("insert test actor token: %v", err)
	}
	return Actor{
		IdentityID: identity.ID,
		TokenID:    tokenID,
		Kind:       identity.Kind,
		Handle:     identity.Handle,
		Operator:   identity.IsOperator,
		RequestID:  "request-" + suffix,
	}, secret
}

func statusForToken(tokens []TokenMetadata, id string) string {
	for _, token := range tokens {
		if token.ID == id {
			return token.Status
		}
	}
	return ""
}
