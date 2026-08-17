//go:build integration

package database

import (
	"errors"
	"testing"

	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestStoreCreateDelegationValidationAndPersistence(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "216")
	identity := insertTestIdentity(t, conn, "217", "delegate-217")
	group, err := store.CreateGroup(t.Context(), operator, GroupCreate{Handle: "delegate-group"})
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "401", "example.com.")

	created, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID:     binding.ID,
		GranteeIdentityID: &identity.ID,
		Selectors: []DelegationSelectorInput{
			{Kind: delegationdomain.SelectorExact, Value: "host.example.com."},
			{Kind: delegationdomain.SelectorGlob, Value: "*.apps.example.com."},
		},
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}
	if created.ID == "" || created.ZoneBindingID != binding.ID || created.GranteeKind != "identity" || created.GranteeID != identity.ID {
		t.Errorf("CreateDelegation() = %+v", created)
	}
	if created.RecordTypes != nil || created.ChangeKinds != nil || len(created.Selectors) != 2 {
		t.Errorf("unrestricted delegation representation = %+v", created)
	}
	assertAuditCount(t, conn, "delegation.create", created.ID, 1)
	var selectorRows, recordTypeRows, changeKindRows int
	if err := conn.QueryRow(t.Context(), `
		SELECT
			(SELECT count(*) FROM delegation_selectors WHERE delegation_id = $1),
			(SELECT count(*) FROM delegation_record_types WHERE delegation_id = $1),
			(SELECT count(*) FROM delegation_change_kinds WHERE delegation_id = $1)`, created.ID).Scan(
		&selectorRows, &recordTypeRows, &changeKindRows,
	); err != nil {
		t.Fatalf("count delegation children: %v", err)
	}
	if selectorRows != 2 || recordTypeRows != 0 || changeKindRows != 0 {
		t.Errorf("delegation child counts = selectors %d, types %d, kinds %d", selectorRows, recordTypeRows, changeKindRows)
	}

	recordTypes := []string{"A", "PTR"}
	changeKinds := []string{"REPLACE", "PRUNE"}
	restricted, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID:  binding.ID,
		GranteeGroupID: &group.ID,
		Selectors: []DelegationSelectorInput{
			{Kind: delegationdomain.SelectorExact, Value: "ptr.example.com."},
		},
		RecordTypes: &recordTypes,
		ChangeKinds: &changeKinds,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(restricted) error = %v", err)
	}
	if len(restricted.RecordTypes) != 2 || len(restricted.ChangeKinds) != 2 || restricted.GranteeKind != "group" {
		t.Errorf("restricted delegation = %+v", restricted)
	}

	empty := []string{}
	invalidInputs := []DelegationCreate{
		{ZoneBindingID: binding.ID, Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "host.example.com."}}},
		{ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID, GranteeGroupID: &group.ID, Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "host.example.com."}}},
		{ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID},
		{ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID, Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "host.example.com."}}, RecordTypes: &empty},
		{ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID, Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "host.example.com."}}, ChangeKinds: &empty},
		{ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID, Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorGlob, Value: "*.outside.test."}}},
	}
	for i, input := range invalidInputs {
		if _, err := store.CreateDelegation(t.Context(), operator, input); !errors.Is(err, ErrInvalid) {
			t.Errorf("CreateDelegation(invalid %d) error = %v, want %v", i, err, ErrInvalid)
		}
	}

	missingID := "10000000-0000-4000-8000-000000000999"
	if _, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID: binding.ID, GranteeIdentityID: &missingID,
		Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "host.example.com."}},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateDelegation(missing grantee) error = %v, want %v", err, ErrNotFound)
	}

	retired := insertTestZoneBinding(t, conn, operator.IdentityID, "402", "retired.example.")
	if _, err := conn.Exec(t.Context(), `UPDATE zone_bindings SET retired_at = now(), retired_by_identity_id = $2 WHERE id = $1`, retired.ID, operator.IdentityID); err != nil {
		t.Fatalf("retire test binding: %v", err)
	}
	if _, err := store.CreateDelegation(t.Context(), operator, DelegationCreate{
		ZoneBindingID: retired.ID, GranteeIdentityID: &identity.ID,
		Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "host.retired.example."}},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateDelegation(retired binding) error = %v, want %v", err, ErrConflict)
	}

	nonOperator := insertTestActorToken(t, conn, identity, "317")
	if _, err := store.CreateDelegation(t.Context(), nonOperator, DelegationCreate{
		ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID,
		Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "host.example.com."}},
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("CreateDelegation(non-operator) error = %v, want %v", err, ErrForbidden)
	}
}

func TestStoreDelegationReadAndRevocationLifecycle(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "218")
	identity := insertTestIdentity(t, conn, "219", "delegate-219")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "403", "example.net.")
	input := DelegationCreate{
		ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID,
		Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorGlob, Value: "*.example.net."}},
	}
	first, err := store.CreateDelegation(t.Context(), operator, input)
	if err != nil {
		t.Fatalf("CreateDelegation(first) error = %v", err)
	}
	second, err := store.CreateDelegation(t.Context(), operator, input)
	if err != nil {
		t.Fatalf("CreateDelegation(overlap) error = %v", err)
	}

	got, err := store.GetDelegation(t.Context(), operator, first.ID)
	if err != nil {
		t.Fatalf("GetDelegation() error = %v", err)
	}
	if got.ID != first.ID || len(got.Selectors) != 1 || got.Selectors[0].Value != "*.example.net." || got.RevokedAt.Valid {
		t.Errorf("GetDelegation() = %+v", got)
	}

	if err := store.RevokeDelegation(t.Context(), operator, first.ID); err != nil {
		t.Fatalf("RevokeDelegation() error = %v", err)
	}
	if err := store.RevokeDelegation(t.Context(), operator, first.ID); err != nil {
		t.Fatalf("RevokeDelegation(repeat) error = %v", err)
	}
	assertAuditCount(t, conn, "delegation.revoke", first.ID, 1)
	revoked, err := store.GetDelegation(t.Context(), operator, first.ID)
	if err != nil || !revoked.RevokedAt.Valid || revoked.Selectors[0] != first.Selectors[0] {
		t.Fatalf("GetDelegation(revoked) = %+v, %v", revoked, err)
	}

	active := true
	activePage, err := store.ListDelegations(t.Context(), operator, DelegationListOptions{
		Limit: 100, ZoneBindingID: &binding.ID, GranteeID: &identity.ID, Active: &active,
	})
	if err != nil {
		t.Fatalf("ListDelegations(active) error = %v", err)
	}
	if len(activePage.Items) != 1 || activePage.Items[0].ID != second.ID {
		t.Errorf("ListDelegations(active) = %+v, want only %s", activePage, second.ID)
	}
	history, err := store.ListDelegations(t.Context(), operator, DelegationListOptions{Limit: 1})
	if err != nil || len(history.Items) != 1 || history.Next == nil {
		t.Fatalf("ListDelegations(history first page) = %+v, %v", history, err)
	}
	historyNext, err := store.ListDelegations(t.Context(), operator, DelegationListOptions{Limit: 1, After: history.Next})
	if err != nil || len(historyNext.Items) != 1 || historyNext.Items[0].ID == history.Items[0].ID {
		t.Fatalf("ListDelegations(history second page) = %+v, %v", historyNext, err)
	}

	nonOperator := insertTestActorToken(t, conn, identity, "319")
	if _, err := store.GetDelegation(t.Context(), nonOperator, first.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("GetDelegation(non-operator) error = %v, want %v", err, ErrForbidden)
	}
}

func insertTestZoneBinding(t *testing.T, conn DBTX, actorID, suffix, zone string) ZoneBinding {
	t.Helper()
	bindingID := "40000000-0000-4000-8000-000000000" + suffix
	var binding ZoneBinding
	err := conn.QueryRow(t.Context(), `
		INSERT INTO zone_bindings (id, generation, upstream, powerdns_zone_id, zone_name, created_by_identity_id)
		VALUES ($1, $2, 'default', $3, $3, $4)
		RETURNING id, generation, upstream, powerdns_zone_id, zone_name,
		          created_by_identity_id, retired_by_identity_id, created_at, retired_at`,
		bindingID, 1, zone, actorID,
	).Scan(
		&binding.ID, &binding.Generation, &binding.Upstream, &binding.PowerDNSZoneID, &binding.ZoneName,
		&binding.CreatedByIdentityID, &binding.RetiredByIdentityID, &binding.CreatedAt, &binding.RetiredAt,
	)
	if err != nil {
		t.Fatalf("insert test zone binding: %v", err)
	}
	return binding
}
