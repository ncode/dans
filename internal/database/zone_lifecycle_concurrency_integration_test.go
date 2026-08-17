//go:build integration

package database

import (
	"bytes"
	"errors"
	"testing"
	"time"

	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestStoreConcurrentLazyBindAndRetirementFailClosed(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	store1, store2 := NewStore(conn1), NewStore(conn2)
	operator := insertTestOperator(t, conn1, "248")
	binding, err := store1.BindCreatedZone(t.Context(), operator, ZoneBindingInput{
		Upstream: "default", PowerDNSZoneID: "lazy-retire-id", ZoneName: "lazy-retire.test.",
	})
	if err != nil {
		t.Fatalf("BindCreatedZone() error = %v", err)
	}

	var plan ZoneDeletionPlan
	errs := runConcurrently(
		func() error {
			var prepareErr error
			plan, prepareErr = store1.PrepareZoneDeletion(t.Context(), operator, ZoneDeletionInput{
				BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x91}, 32), Deadline: time.Now().Add(time.Minute),
			})
			return prepareErr
		},
		func() error {
			_, ensureErr := store2.EnsureZoneBinding(t.Context(), operator, ZoneBindingInput{
				Upstream: binding.Upstream, PowerDNSZoneID: binding.PowerDNSZoneID, ZoneName: binding.ZoneName,
			})
			return ensureErr
		},
	)
	if errs[0] != nil {
		t.Fatalf("PrepareZoneDeletion() concurrent error = %v", errs[0])
	}
	if errs[1] != nil && !errors.Is(errs[1], ErrConflict) {
		t.Fatalf("EnsureZoneBinding() concurrent error = %v, want nil or %v", errs[1], ErrConflict)
	}
	if !plan.Binding.RetiredAt.Valid {
		t.Fatalf("concurrent deletion plan binding = %+v", plan.Binding)
	}
	var active, lifetimes int
	if err := conn1.QueryRow(t.Context(), `
		SELECT
			count(*) FILTER (WHERE retired_at IS NULL),
			count(*)
		FROM zone_bindings
		WHERE upstream = $1 AND zone_name = $2`, binding.Upstream, binding.ZoneName,
	).Scan(&active, &lifetimes); err != nil {
		t.Fatalf("read lazy-bind retirement result: %v", err)
	}
	if active != 0 || lifetimes != 1 {
		t.Errorf("lazy-bind retirement result = active %d, lifetimes %d; want 0, 1", active, lifetimes)
	}
}

func TestStoreConcurrentDelegationCreationAndRetirementFailClosed(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	store1, store2 := NewStore(conn1), NewStore(conn2)
	operator := insertTestOperator(t, conn1, "249")
	identity := insertTestIdentity(t, conn1, "250", "retirement-race-writer")
	binding, err := store1.BindCreatedZone(t.Context(), operator, ZoneBindingInput{
		Upstream: "default", PowerDNSZoneID: "grant-retire-id", ZoneName: "grant-retire.test.",
	})
	if err != nil {
		t.Fatalf("BindCreatedZone() error = %v", err)
	}

	errs := runConcurrently(
		func() error {
			_, prepareErr := store1.PrepareZoneDeletion(t.Context(), operator, ZoneDeletionInput{
				BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x92}, 32), Deadline: time.Now().Add(time.Minute),
			})
			return prepareErr
		},
		func() error {
			_, createErr := store2.CreateDelegation(t.Context(), operator, DelegationCreate{
				ZoneBindingID: binding.ID, GranteeIdentityID: &identity.ID,
				Selectors: []DelegationSelectorInput{{Kind: delegationdomain.SelectorGlob, Value: "*.grant-retire.test."}},
			})
			return createErr
		},
	)
	if errs[0] != nil {
		t.Fatalf("PrepareZoneDeletion() concurrent error = %v", errs[0])
	}
	if errs[1] != nil && !errors.Is(errs[1], ErrConflict) {
		t.Fatalf("CreateDelegation() concurrent error = %v, want nil or %v", errs[1], ErrConflict)
	}
	var retired bool
	var activeDelegations int
	if err := conn1.QueryRow(t.Context(), `
		SELECT
			(SELECT retired_at IS NOT NULL FROM zone_bindings WHERE id = $1),
			(SELECT count(*) FROM delegations WHERE zone_binding_id = $1 AND revoked_at IS NULL)`, binding.ID,
	).Scan(&retired, &activeDelegations); err != nil {
		t.Fatalf("read delegation-retirement result: %v", err)
	}
	if !retired || activeDelegations != 0 {
		t.Errorf("delegation-retirement result = retired %t, active grants %d; want true, 0", retired, activeDelegations)
	}
}
