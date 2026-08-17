//go:build integration

package database

import (
	"errors"
	"testing"
)

func TestStoreGroupAndMembershipLifecycle(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	actor := insertTestOperator(t, conn, "202")
	member := insertTestIdentity(t, conn, "203", "member-203")

	displayName := "DNS Editors"
	created, err := store.CreateGroup(t.Context(), actor, GroupCreate{
		Handle:      "dns-editors",
		DisplayName: &displayName,
	})
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	if created.ID == "" || created.Handle != "dns-editors" || !created.Enabled {
		t.Errorf("CreateGroup() = %+v", created)
	}
	assertAuditCount(t, conn, "group.create", created.ID, 1)

	if _, err := store.CreateGroup(t.Context(), actor, GroupCreate{Handle: "dns-editors"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateGroup(duplicate handle) error = %v, want %v", err, ErrConflict)
	}
	got, err := store.GetGroup(t.Context(), actor, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("GetGroup() = %+v, %v", got, err)
	}
	groups, err := store.ListGroups(t.Context(), actor, GroupListOptions{Limit: 100})
	if err != nil || len(groups.Items) != 1 {
		t.Fatalf("ListGroups() = %+v, %v, want one item", groups, err)
	}
	nested, err := store.CreateGroup(t.Context(), actor, GroupCreate{Handle: "nested-group"})
	if err != nil {
		t.Fatalf("CreateGroup(nested candidate) error = %v", err)
	}
	if _, err := store.AddGroupMember(t.Context(), actor, created.ID, nested.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("AddGroupMember(group as identity) error = %v, want %v", err, ErrInvalid)
	}

	membership, err := store.AddGroupMember(t.Context(), actor, created.ID, member.ID)
	if err != nil {
		t.Fatalf("AddGroupMember() error = %v", err)
	}
	if membership.GroupID != created.ID || membership.IdentityID != member.ID {
		t.Errorf("AddGroupMember() = %+v", membership)
	}
	if _, err := store.AddGroupMember(t.Context(), actor, created.ID, member.ID); err != nil {
		t.Fatalf("AddGroupMember(repeat) error = %v", err)
	}
	assertAuditCount(t, conn, "group.member.add", created.ID+":"+member.ID, 1)

	disabled := false
	patched, err := store.PatchGroup(t.Context(), actor, created.ID, GroupPatch{Enabled: &disabled})
	if err != nil {
		t.Fatalf("PatchGroup(disable) error = %v", err)
	}
	if patched.Enabled {
		t.Errorf("PatchGroup(disable) = %+v", patched)
	}
	var retained, effective int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM group_memberships WHERE group_id = $1`, created.ID).Scan(&retained); err != nil {
		t.Fatalf("count retained membership: %v", err)
	}
	if err := conn.QueryRow(t.Context(), `
		SELECT count(*)
		FROM group_memberships AS m
		JOIN groups AS g ON g.id = m.group_id AND g.enabled = true
		WHERE m.group_id = $1`, created.ID).Scan(&effective); err != nil {
		t.Fatalf("count effective membership: %v", err)
	}
	if retained != 1 || effective != 0 {
		t.Errorf("disabled group membership counts = retained %d, effective %d, want 1, 0", retained, effective)
	}

	if err := store.RemoveGroupMember(t.Context(), actor, created.ID, member.ID); err != nil {
		t.Fatalf("RemoveGroupMember() error = %v", err)
	}
	if err := store.RemoveGroupMember(t.Context(), actor, created.ID, member.ID); err != nil {
		t.Fatalf("RemoveGroupMember(repeat) error = %v", err)
	}
	assertAuditCount(t, conn, "group.member.remove", created.ID+":"+member.ID, 1)
}

func insertTestIdentity(t *testing.T, conn DBTX, suffix, handle string) Identity {
	t.Helper()
	id := "10000000-0000-4000-8000-000000000" + suffix
	var identity Identity
	err := conn.QueryRow(t.Context(), `
		INSERT INTO identities (id, kind, handle)
		VALUES ($1, 'user', $2)
		RETURNING id, kind, handle, display_name, enabled, is_operator, created_at, updated_at`, id, handle).Scan(
		&identity.ID, &identity.Kind, &identity.Handle, &identity.DisplayName,
		&identity.Enabled, &identity.IsOperator, &identity.CreatedAt, &identity.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("insert test identity: %v", err)
	}
	return identity
}
