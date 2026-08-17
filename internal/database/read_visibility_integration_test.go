//go:build integration

package database

import (
	"errors"
	"testing"

	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestStorePaginatedReadVisibility(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "207")
	ownerIdentity := insertTestIdentity(t, conn, "208", "owner-208")
	owner := insertTestActorToken(t, conn, ownerIdentity, "308")
	members := []Identity{
		ownerIdentity,
		insertTestIdentity(t, conn, "209", "member-209"),
		insertTestIdentity(t, conn, "210", "member-210"),
	}

	current, err := store.GetCurrentIdentity(t.Context(), owner)
	if err != nil || current.ID != owner.IdentityID {
		t.Fatalf("GetCurrentIdentity() = %+v, %v", current, err)
	}
	if _, err := store.GetIdentity(t.Context(), owner, operator.IdentityID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("GetIdentity(non-operator) error = %v, want %v", err, ErrForbidden)
	}
	if _, err := store.ListIdentities(t.Context(), owner, IdentityListOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListIdentities(non-operator) error = %v, want %v", err, ErrForbidden)
	}
	firstIdentities, err := store.ListIdentities(t.Context(), operator, IdentityListOptions{Limit: 2})
	if err != nil || len(firstIdentities.Items) != 2 || firstIdentities.Next == nil {
		t.Fatalf("ListIdentities(first page) = %+v, %v", firstIdentities, err)
	}
	secondIdentities, err := store.ListIdentities(t.Context(), operator, IdentityListOptions{Limit: 2, After: firstIdentities.Next})
	if err != nil || len(secondIdentities.Items) != 2 {
		t.Fatalf("ListIdentities(second page) = %+v, %v", secondIdentities, err)
	}
	assertDistinctPageBoundary(t, firstIdentities.Items[1].ID, secondIdentities.Items[0].ID)

	groups := make([]Group, 3)
	for i, handle := range []string{"group-207", "group-208", "group-209"} {
		groups[i], err = store.CreateGroup(t.Context(), operator, GroupCreate{Handle: handle})
		if err != nil {
			t.Fatalf("CreateGroup(%q) error = %v", handle, err)
		}
		if _, err := store.AddGroupMember(t.Context(), operator, groups[i].ID, ownerIdentity.ID); err != nil {
			t.Fatalf("AddGroupMember(%q, owner) error = %v", handle, err)
		}
	}
	disabled := false
	if _, err := store.PatchGroup(t.Context(), operator, groups[1].ID, GroupPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("PatchGroup(disable) error = %v", err)
	}

	firstGroups, err := store.ListCurrentIdentityGroups(t.Context(), owner, GroupListOptions{Limit: 2})
	if err != nil || len(firstGroups.Items) != 2 || firstGroups.Next == nil {
		t.Fatalf("ListCurrentIdentityGroups(first) = %+v, %v", firstGroups, err)
	}
	secondGroups, err := store.ListCurrentIdentityGroups(t.Context(), owner, GroupListOptions{Limit: 2, After: firstGroups.Next})
	if err != nil || len(secondGroups.Items) != 1 {
		t.Fatalf("ListCurrentIdentityGroups(second) = %+v, %v", secondGroups, err)
	}
	allGroups := append(append([]Group{}, firstGroups.Items...), secondGroups.Items...)
	if !containsGroupEnabled(allGroups, groups[1].ID, false) {
		t.Errorf("self groups omit retained disabled membership: %+v", allGroups)
	}

	for _, member := range members[1:] {
		if _, err := store.AddGroupMember(t.Context(), operator, groups[0].ID, member.ID); err != nil {
			t.Fatalf("AddGroupMember(%s) error = %v", member.ID, err)
		}
	}
	firstMembers, err := store.ListGroupMembers(t.Context(), operator, groups[0].ID, IdentityListOptions{Limit: 2})
	if err != nil || len(firstMembers.Items) != 2 || firstMembers.Next == nil {
		t.Fatalf("ListGroupMembers(first) = %+v, %v", firstMembers, err)
	}
	secondMembers, err := store.ListGroupMembers(t.Context(), operator, groups[0].ID, IdentityListOptions{Limit: 2, After: firstMembers.Next})
	if err != nil || len(secondMembers.Items) != 1 {
		t.Fatalf("ListGroupMembers(second) = %+v, %v", secondMembers, err)
	}
	assertDistinctPageBoundary(t, firstMembers.Items[1].ID, secondMembers.Items[0].ID)
}

func TestStoreCurrentIdentityEffectiveDelegationVisibility(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "228")
	identity := insertTestIdentity(t, conn, "229", "self-229")
	actor := insertTestActorToken(t, conn, identity, "329")
	other := insertTestIdentity(t, conn, "230", "other-230")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "409", "self.test.")
	direct1 := createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "one.self.test."}}, nil, nil)
	direct2 := createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "two.self.test."}}, nil, nil)
	createIdentityGrant(t, store, operator, binding.ID, other.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "other.self.test."}}, nil, nil)
	group, err := store.CreateGroup(t.Context(), operator, GroupCreate{Handle: "self-group"})
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	if _, err := store.AddGroupMember(t.Context(), operator, group.ID, identity.ID); err != nil {
		t.Fatalf("AddGroupMember() error = %v", err)
	}
	groupGrant, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID: binding.ID, GranteeGroupID: &group.ID,
		Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorGlob, Value: "*.self.test."}},
	})
	if err != nil {
		t.Fatalf("CreateDelegation(group) error = %v", err)
	}

	first, err := store.ListCurrentIdentityDelegations(t.Context(), actor, DelegationListOptions{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.Next == nil {
		t.Fatalf("ListCurrentIdentityDelegations(first) = %+v, %v", first, err)
	}
	second, err := store.ListCurrentIdentityDelegations(t.Context(), actor, DelegationListOptions{Limit: 2, After: first.Next})
	if err != nil || len(second.Items) != 1 {
		t.Fatalf("ListCurrentIdentityDelegations(second) = %+v, %v", second, err)
	}
	visible := append(append([]DelegationDetails{}, first.Items...), second.Items...)
	if !containsDelegation(visible, direct1.ID) || !containsDelegation(visible, direct2.ID) || !containsDelegation(visible, groupGrant.ID) {
		t.Errorf("effective delegation page items = %+v", visible)
	}

	disabled := false
	if _, err := store.PatchGroup(t.Context(), operator, group.ID, GroupPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("PatchGroup(disable) error = %v", err)
	}
	afterDisable, err := store.ListCurrentIdentityDelegations(t.Context(), actor, DelegationListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListCurrentIdentityDelegations(disabled group) error = %v", err)
	}
	if containsDelegation(afterDisable.Items, groupGrant.ID) || len(afterDisable.Items) != 2 {
		t.Errorf("effective delegations after group disable = %+v", afterDisable.Items)
	}
	if err := store.RevokeDelegation(t.Context(), operator, direct1.ID); err != nil {
		t.Fatalf("RevokeDelegation() error = %v", err)
	}
	afterRevoke, err := store.ListCurrentIdentityDelegations(t.Context(), actor, DelegationListOptions{Limit: 100})
	if err != nil || len(afterRevoke.Items) != 1 || afterRevoke.Items[0].ID != direct2.ID {
		t.Errorf("effective delegations after revoke = %+v, %v", afterRevoke.Items, err)
	}
}

func containsDelegation(items []DelegationDetails, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func assertDistinctPageBoundary(t *testing.T, before, after string) {
	t.Helper()
	if before == after {
		t.Fatalf("page boundary repeated resource %s", before)
	}
}

func containsGroupEnabled(groups []Group, id string, enabled bool) bool {
	for _, group := range groups {
		if group.ID == id && group.Enabled == enabled {
			return true
		}
	}
	return false
}
