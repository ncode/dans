//go:build integration

package database

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestManagementReadsPrefixAndRetainedAuthority(t *testing.T) {
	t.Parallel()
	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "701")
	identity := insertTestIdentity(t, conn, "702", "svc_one")
	insertTestIdentity(t, conn, "703", "svcxtwo")
	actor := insertTestActorToken(t, conn, identity, "704")
	prefix := "svc_"
	identities, err := store.ListIdentities(t.Context(), operator, IdentityListOptions{HandlePrefix: &prefix})
	if err != nil || len(identities.Items) != 1 || identities.Items[0].ID != identity.ID {
		t.Fatalf("literal prefix = %+v, %v", identities, err)
	}
	for i, handle := range []string{"svc.one", "svc-one"} {
		created, err := store.CreateIdentity(t.Context(), operator, IdentityCreate{Kind: "service", Handle: handle})
		if err != nil {
			t.Fatal(err)
		}
		match := handle[:4]
		found, err := store.ListIdentities(t.Context(), operator, IdentityListOptions{HandlePrefix: &match})
		if err != nil || len(found.Items) != 1 || found.Items[0].ID != created.ID {
			t.Fatalf("punctuation prefix %d: %+v %v", i, found, err)
		}
	}
	exact := "svcxtwo"
	found, err := store.ListIdentities(t.Context(), operator, IdentityListOptions{Handle: &exact, HandlePrefix: &prefix})
	if err != nil || len(found.Items) != 0 {
		t.Fatalf("exact and prefix must intersect: %+v %v", found, err)
	}

	for _, bad := range []string{"", "%", "Svc", "_svc", "svc\\"} {
		if _, err := store.ListIdentities(t.Context(), operator, IdentityListOptions{HandlePrefix: &bad}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid prefix %q: %v", bad, err)
		}
	}
	group, err := store.CreateGroup(t.Context(), operator, GroupCreate{Handle: "svc_group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddGroupMember(t.Context(), operator, group.ID, identity.ID); err != nil {
		t.Fatal(err)
	}
	members, err := store.ListGroupMembers(t.Context(), operator, group.ID, IdentityListOptions{HandlePrefix: &prefix})
	if err != nil || len(members.Items) != 1 {
		t.Fatalf("members = %+v, %v", members, err)
	}
	groups, err := store.ListIdentityGroups(t.Context(), operator, identity.ID, GroupListOptions{HandlePrefix: &prefix})
	if err != nil || len(groups.Items) != 1 {
		t.Fatalf("groups = %+v, %v", groups, err)
	}
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "705", "management.test.")
	direct := createIdentityGrant(t, store, operator, binding.ID, identity.ID, []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "one.management.test."}}, nil, nil)
	groupGrant, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{ZoneBindingID: binding.ID, GranteeGroupID: &group.ID, Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "two.management.test."}}})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := store.PatchGroup(t.Context(), operator, group.ID, GroupPatch{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	effective, err := store.ListIdentityDelegations(t.Context(), operator, identity.ID, DelegationListOptions{})
	if err != nil || len(effective.Items) != 1 || effective.Items[0].ID != direct.ID {
		t.Fatalf("effective = %+v, %v", effective, err)
	}
	retained, err := store.ListIdentityAssignments(t.Context(), operator, identity.ID, DelegationListOptions{})
	if err != nil || len(retained.Items) != 2 {
		t.Fatalf("retained = %+v, %v", retained, err)
	}
	for _, item := range retained.Items {
		if item.ID == groupGrant.ID && (item.Effective || item.GroupEnabled == nil || *item.GroupEnabled || item.GroupHandle == nil || *item.GroupHandle != group.Handle) {
			t.Fatalf("suspended group state = %+v", item)
		}
	}
	if _, err := store.PatchIdentity(t.Context(), operator, identity.ID, IdentityPatch{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	effective, err = store.ListIdentityDelegations(t.Context(), operator, identity.ID, DelegationListOptions{})
	if err != nil || len(effective.Items) != 0 {
		t.Fatalf("disabled identity effective = %+v, %v", effective, err)
	}
	if err := store.RevokeDelegation(t.Context(), operator, direct.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), "UPDATE zone_bindings SET retired_at=clock_timestamp(), retired_by_identity_id=$2 WHERE id=$1", binding.ID, operator.IdentityID); err != nil {
		t.Fatal(err)
	}
	retained, err = store.ListIdentityAssignments(t.Context(), operator, identity.ID, DelegationListOptions{Limit: 1})
	if err != nil || len(retained.Items) != 1 || retained.Next == nil {
		t.Fatalf("retained first page = %+v, %v", retained, err)
	}
	second, err := store.ListIdentityAssignments(t.Context(), operator, identity.ID, DelegationListOptions{Limit: 1, After: retained.Next})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == retained.Items[0].ID {
		t.Fatalf("retained second page = %+v, %v", second, err)
	}
	for _, item := range append(retained.Items, second.Items...) {
		if item.Effective || item.IdentityEnabled || item.BindingStatus != "retired" {
			t.Fatalf("retired state = %+v", item)
		}
	}
	if _, err := store.ListIdentityGroups(t.Context(), actor, identity.ID, GroupListOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nonoperator groups: %v", err)
	}
	if _, err := store.ListIdentityDelegations(t.Context(), actor, identity.ID, DelegationListOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nonoperator effective: %v", err)
	}
	if _, err := store.ListIdentityAssignments(t.Context(), actor, identity.ID, DelegationListOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nonoperator retained: %v", err)
	}
}

func TestBindingRecoveryReportsExistingEligibility(t *testing.T) {
	t.Parallel()
	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "711")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "712", "recovery.test.")
	state, err := store.GetZoneBindingRecovery(t.Context(), operator, binding.ID)
	if err != nil || state.DeletionState != "no_attempt" || len(state.Actions) != 1 || state.Actions[0] != "observe" {
		t.Fatalf("active recovery = %+v, %v", state, err)
	}
}

func TestBindingRecoveryStaleActionsStillConflict(t *testing.T) {
	t.Parallel()
	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "721")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "722", "stale.test.")
	input := ZoneDeletionInput{BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{1}, 32), Deadline: time.Now().Add(time.Minute)}
	plan, err := store.PrepareZoneDeletion(t.Context(), operator, input)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.GetZoneBindingRecovery(t.Context(), operator, binding.ID)
	if err != nil || state.DeletionState != "pending" || slices.Contains(state.Actions, "retry_delete") {
		t.Fatalf("pending=%+v %v", state, err)
	}
	code := 500
	if _, err := store.RecordZoneDeletionOutcome(t.Context(), plan, ZoneDeletionOutcomeInput{Result: ZoneDeletionFailed, ResponseCode: &code, ResponseClass: "server_error"}); err != nil {
		t.Fatal(err)
	}
	state, err = store.GetZoneBindingRecovery(t.Context(), operator, binding.ID)
	if err != nil || state.DeletionState != "failed" || !slices.Contains(state.Actions, "retry_delete") {
		t.Fatalf("failed=%+v %v", state, err)
	}
	retry, err := store.PrepareZoneDeletionRetry(t.Context(), operator, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareZoneDeletionRetry(t.Context(), operator, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale retry action allowed: %v", err)
	}
	if _, err := store.RecordZoneDeletionOutcome(t.Context(), retry, ZoneDeletionOutcomeInput{Result: ZoneDeletionUnknown}); err != nil {
		t.Fatal(err)
	}
	state, err = store.GetZoneBindingRecovery(t.Context(), operator, binding.ID)
	if err != nil || state.DeletionState != "unknown" {
		t.Fatalf("unknown=%+v %v", state, err)
	}
	if _, err := store.ConfirmZoneBindingAbsent(t.Context(), operator, binding.ID); err != nil {
		t.Fatal(err)
	}
	state, err = store.GetZoneBindingRecovery(t.Context(), operator, binding.ID)
	if err != nil || state.DeletionState != "confirmed_absent" || slices.Contains(state.Actions, "retry_delete") {
		t.Fatalf("confirmed=%+v %v", state, err)
	}
	if _, err := store.PrepareZoneDeletionRetry(t.Context(), operator, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale action bypassed confirmation: %v", err)
	}
}
