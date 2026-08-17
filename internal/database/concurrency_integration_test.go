//go:build integration

package database

import (
	"errors"
	"sync"
	"testing"
)

func TestStoreConcurrentOperatorDemotionsPreserveOneEnabledOperator(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	actor1 := insertTestOperator(t, conn1, "211")
	actor2 := insertTestOperator(t, conn1, "212")
	actor2.RequestID = "request-demote-212"
	demote := false

	errs := runConcurrently(
		func() error {
			_, err := NewStore(conn1).PatchIdentity(t.Context(), actor1, actor1.IdentityID, IdentityPatch{Operator: &demote})
			return err
		},
		func() error {
			_, err := NewStore(conn2).PatchIdentity(t.Context(), actor2, actor2.IdentityID, IdentityPatch{Operator: &demote})
			return err
		},
	)
	if countErrors(errs, nil) != 1 || countErrors(errs, ErrConflict) != 1 {
		t.Fatalf("concurrent demotion errors = %v, want one success and one conflict", errs)
	}
	var enabledOperators int
	if err := conn1.QueryRow(t.Context(), `SELECT count(*) FROM identities WHERE enabled AND is_operator`).Scan(&enabledOperators); err != nil {
		t.Fatalf("count enabled operators: %v", err)
	}
	if enabledOperators != 1 {
		t.Errorf("enabled operator count = %d, want 1", enabledOperators)
	}
}

func TestStoreConcurrentUniquenessMembershipAndIdentityState(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	store1, store2 := NewStore(conn1), NewStore(conn2)
	operator := insertTestOperator(t, conn1, "213")
	operator2 := operator
	operator2.RequestID = "request-concurrent-213-b"
	member := insertTestIdentity(t, conn1, "214", "member-214")
	memberActor, memberSecret := insertTestActorTokenWithSecret(t, conn1, member, "314")

	identityErrs := runConcurrently(
		func() error {
			_, err := store1.CreateIdentity(t.Context(), operator, IdentityCreate{Kind: "user", Handle: "same-handle"})
			return err
		},
		func() error {
			_, err := store2.CreateIdentity(t.Context(), operator2, IdentityCreate{Kind: "service", Handle: "same-handle"})
			return err
		},
	)
	assertOneSuccessOneConflict(t, "identity handle", identityErrs)

	groupErrs := runConcurrently(
		func() error {
			_, err := store1.CreateGroup(t.Context(), operator, GroupCreate{Handle: "same-group"})
			return err
		},
		func() error {
			_, err := store2.CreateGroup(t.Context(), operator2, GroupCreate{Handle: "same-group"})
			return err
		},
	)
	assertOneSuccessOneConflict(t, "group handle", groupErrs)

	tokenResults := make([]CreatedToken, 2)
	tokenErrs := runConcurrently(
		func() error {
			var err error
			tokenResults[0], err = store1.CreateToken(t.Context(), operator, member.ID, TokenCreate{Label: "same-label"})
			return err
		},
		func() error {
			var err error
			tokenResults[1], err = store2.CreateToken(t.Context(), operator2, member.ID, TokenCreate{Label: "same-label"})
			return err
		},
	)
	assertOneSuccessOneConflict(t, "active token label", tokenErrs)
	for i, err := range tokenErrs {
		if errors.Is(err, ErrConflict) && tokenResults[i].Secret != "" {
			t.Errorf("conflicting token creation returned a secret")
		}
	}

	group, err := store1.CreateGroup(t.Context(), operator, GroupCreate{Handle: "race-members"})
	if err != nil {
		t.Fatalf("CreateGroup(race-members) error = %v", err)
	}
	addErrs := runConcurrently(
		func() error { _, err := store1.AddGroupMember(t.Context(), operator, group.ID, member.ID); return err },
		func() error { _, err := store2.AddGroupMember(t.Context(), operator2, group.ID, member.ID); return err },
	)
	if countErrors(addErrs, nil) != 2 {
		t.Fatalf("concurrent membership add errors = %v, want two successes", addErrs)
	}
	assertMembershipRowsAndAudits(t, conn1, group.ID, member.ID, 1, "group.member.add", 1)

	removeErrs := runConcurrently(
		func() error { return store1.RemoveGroupMember(t.Context(), operator, group.ID, member.ID) },
		func() error { return store2.RemoveGroupMember(t.Context(), operator2, group.ID, member.ID) },
	)
	if countErrors(removeErrs, nil) != 2 {
		t.Fatalf("concurrent membership remove errors = %v, want two successes", removeErrs)
	}
	assertMembershipRowsAndAudits(t, conn1, group.ID, member.ID, 0, "group.member.remove", 1)

	disabled := false
	if _, err := store1.PatchIdentity(t.Context(), operator, member.ID, IdentityPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("PatchIdentity(disable) error = %v", err)
	}
	if _, err := store2.Authenticate(t.Context(), memberSecret); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate(disabled owner) error = %v, want %v", err, ErrUnauthenticated)
	}
	enabled := true
	if _, err := store2.PatchIdentity(t.Context(), operator2, member.ID, IdentityPatch{Enabled: &enabled}); err != nil {
		t.Fatalf("PatchIdentity(re-enable) error = %v", err)
	}
	if authenticated, err := store1.Authenticate(t.Context(), memberSecret); err != nil || authenticated.IdentityID != memberActor.IdentityID {
		t.Fatalf("Authenticate(re-enabled owner) = %+v, %v", authenticated, err)
	}
}

func runConcurrently(functions ...func() error) []error {
	start := make(chan struct{})
	results := make([]error, len(functions))
	var ready sync.WaitGroup
	ready.Add(len(functions))
	var done sync.WaitGroup
	done.Add(len(functions))
	for i, function := range functions {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			results[i] = function()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return results
}

func countErrors(errs []error, target error) int {
	count := 0
	for _, err := range errs {
		if target == nil && err == nil || target != nil && errors.Is(err, target) {
			count++
		}
	}
	return count
}

func assertOneSuccessOneConflict(t *testing.T, name string, errs []error) {
	t.Helper()
	if countErrors(errs, nil) != 1 || countErrors(errs, ErrConflict) != 1 {
		t.Fatalf("concurrent %s errors = %v, want one success and one conflict", name, errs)
	}
}

func assertMembershipRowsAndAudits(t *testing.T, conn DBTX, groupID, identityID string, wantRows int, action string, wantAudits int) {
	t.Helper()
	var rows int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM group_memberships WHERE group_id = $1 AND identity_id = $2`, groupID, identityID).Scan(&rows); err != nil {
		t.Fatalf("count group memberships: %v", err)
	}
	if rows != wantRows {
		t.Errorf("membership rows = %d, want %d", rows, wantRows)
	}
	assertAuditCount(t, conn, action, groupID+":"+identityID, wantAudits)
}
