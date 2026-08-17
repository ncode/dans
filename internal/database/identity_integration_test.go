//go:build integration

package database

import (
	"errors"
	"testing"

	"github.com/ncode/dans/internal/identifier"
)

func TestStoreIdentityLifecycle(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "201")

	displayName := "Alice Example"
	created, err := store.CreateIdentity(t.Context(), actor, IdentityCreate{
		Kind:        "user",
		Handle:      "alice",
		DisplayName: &displayName,
	})
	if err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}
	if created.ID == "" || created.Kind != "user" || created.Handle != "alice" || !created.Enabled || created.IsOperator {
		t.Errorf("CreateIdentity() = %+v", created)
	}

	var tokenCount int
	if err := conn.QueryRow(t.Context(), "SELECT count(*) FROM api_tokens WHERE identity_id = $1", created.ID).Scan(&tokenCount); err != nil {
		t.Fatalf("count implicit tokens: %v", err)
	}
	if tokenCount != 0 {
		t.Errorf("implicit token count = %d, want 0", tokenCount)
	}
	assertAuditCount(t, conn, "identity.create", created.ID, 1)

	if _, err := store.CreateIdentity(t.Context(), actor, IdentityCreate{Kind: "service", Handle: "alice"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateIdentity(duplicate handle) error = %v, want %v", err, ErrConflict)
	}

	got, err := store.GetIdentity(t.Context(), actor, created.ID)
	if err != nil {
		t.Fatalf("GetIdentity() error = %v", err)
	}
	if got.ID != created.ID || got.Handle != created.Handle {
		t.Errorf("GetIdentity() = %+v, want %+v", got, created)
	}

	page, err := store.ListIdentities(t.Context(), actor, IdentityListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListIdentities() error = %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("ListIdentities() item count = %d, want 2", len(page.Items))
	}

	nullDisplay := NullableStringPatch{Set: true}
	disabled := false
	patched, err := store.PatchIdentity(t.Context(), actor, created.ID, IdentityPatch{
		DisplayName: nullDisplay,
		Enabled:     &disabled,
	})
	if err != nil {
		t.Fatalf("PatchIdentity() error = %v", err)
	}
	if patched.DisplayName != nil || patched.Enabled {
		t.Errorf("PatchIdentity() = %+v, want null display and disabled", patched)
	}
	assertAuditCount(t, conn, "identity.update", created.ID, 1)

	demote := false
	if _, err := store.PatchIdentity(t.Context(), actor, actor.IdentityID, IdentityPatch{Operator: &demote}); !errors.Is(err, ErrConflict) {
		t.Fatalf("PatchIdentity(last operator) error = %v, want %v", err, ErrConflict)
	}
	operator, err := store.GetIdentity(t.Context(), actor, actor.IdentityID)
	if err != nil {
		t.Fatalf("GetIdentity(operator) error = %v", err)
	}
	if !operator.Enabled || !operator.IsOperator {
		t.Errorf("last operator changed after rejection: %+v", operator)
	}
}

func insertTestOperator(t *testing.T, conn DBTX, suffix string) Actor {
	t.Helper()
	actor, _ := insertTestOperatorWithSecret(t, conn, suffix)
	return actor
}

func insertTestOperatorWithSecret(t *testing.T, conn DBTX, suffix string) (Actor, string) {
	t.Helper()

	identityID := "10000000-0000-4000-8000-000000000" + suffix
	tokenID := "30000000-0000-4000-8000-000000000" + suffix
	handle := "operator-" + suffix
	if _, err := conn.Exec(t.Context(), `
		INSERT INTO identities (id, kind, handle, is_operator)
		VALUES ($1, 'user', $2, true)`, identityID, handle); err != nil {
		t.Fatalf("insert test operator: %v", err)
	}
	secret, digest, err := identifier.NewToken()
	if err != nil {
		t.Fatalf("identifier.NewToken() error = %v", err)
	}
	if _, err := conn.Exec(t.Context(), `
		INSERT INTO api_tokens (id, identity_id, label, digest)
		VALUES ($1, $2, 'test', $3)`, tokenID, identityID, digest[:]); err != nil {
		t.Fatalf("insert test operator token: %v", err)
	}
	return Actor{
		IdentityID: identityID,
		TokenID:    tokenID,
		Kind:       "user",
		Handle:     handle,
		Operator:   true,
		RequestID:  "request-" + suffix,
	}, secret
}

func assertAuditCount(t *testing.T, conn DBTX, action, targetID string, want int) {
	t.Helper()

	var got int
	if err := conn.QueryRow(t.Context(), `
		SELECT count(*) FROM audit_events
		WHERE action = $1 AND target_id = $2`, action, targetID).Scan(&got); err != nil {
		t.Fatalf("count audit events for %s: %v", action, err)
	}
	if got != want {
		t.Errorf("audit count for %s/%s = %d, want %d", action, targetID, got, want)
	}
}
