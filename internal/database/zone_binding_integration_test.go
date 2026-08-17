//go:build integration

package database

import (
	"errors"
	"testing"
)

func TestStoreZoneBindingCreationAndConcurrentConvergence(t *testing.T) {
	t.Parallel()

	conn1, schema, dsn := newTestSchema(t)
	if err := Migrate(t.Context(), conn1); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	conn2 := connectToTestSchema(t, dsn, schema)
	store1, store2 := NewStore(conn1), NewStore(conn2)
	operator := insertTestOperator(t, conn1, "238")
	operator2 := operator
	operator2.RequestID = "zone-binding-concurrent-2"
	input := ZoneBindingInput{Upstream: "default", PowerDNSZoneID: "Example.COM.", ZoneName: "Example.COM."}
	results := make([]ZoneBinding, 2)
	errs := runConcurrently(
		func() error {
			var err error
			results[0], err = store1.BindCreatedZone(t.Context(), operator, input)
			return err
		},
		func() error {
			var err error
			results[1], err = store2.BindCreatedZone(t.Context(), operator2, input)
			return err
		},
	)
	if countErrors(errs, nil) != 2 {
		t.Fatalf("BindCreatedZone(concurrent) errors = %v", errs)
	}
	if results[0].ID != results[1].ID || results[0].Generation != 1 || results[1].Generation != 1 || results[0].ZoneName != "example.com." {
		t.Errorf("converged bindings = %+v / %+v", results[0], results[1])
	}
	assertAuditCount(t, conn1, "zone_binding.create", results[0].ID, 1)
	var rows int
	if err := conn1.QueryRow(t.Context(), `SELECT count(*) FROM zone_bindings WHERE upstream = 'default' AND zone_name = 'example.com.'`).Scan(&rows); err != nil {
		t.Fatalf("count converged bindings: %v", err)
	}
	if rows != 1 {
		t.Errorf("converged binding rows = %d, want 1", rows)
	}

	got, err := store1.GetZoneBinding(t.Context(), operator, results[0].ID)
	if err != nil || got.ID != results[0].ID {
		t.Fatalf("GetZoneBinding() = %+v, %v", got, err)
	}
	activeStatus := "active"
	active, err := store1.ListZoneBindings(t.Context(), operator, ZoneBindingListOptions{Limit: 100, Status: &activeStatus})
	if err != nil || len(active.Items) != 1 || active.Items[0].ID != results[0].ID {
		t.Fatalf("ListZoneBindings(active) = %+v, %v", active, err)
	}

	if _, err := conn1.Exec(t.Context(), `UPDATE zone_bindings SET retired_at = now(), retired_by_identity_id = $2 WHERE id = $1`, results[0].ID, operator.IdentityID); err != nil {
		t.Fatalf("retire first binding: %v", err)
	}
	if _, err := store1.EnsureZoneBinding(t.Context(), operator, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("EnsureZoneBinding(historical zone) error = %v, want %v", err, ErrConflict)
	}
	next, err := store1.BindCreatedZone(t.Context(), operator, input)
	if err != nil {
		t.Fatalf("BindCreatedZone(next lifetime) error = %v", err)
	}
	if next.ID == results[0].ID || next.Generation != 2 {
		t.Errorf("next binding = %+v, want new ID/generation 2", next)
	}
	old, err := store1.GetZoneBinding(t.Context(), operator, results[0].ID)
	if err != nil || old.Generation != 1 || !old.RetiredAt.Valid {
		t.Errorf("historical binding changed = %+v, %v", old, err)
	}

	existingInput := ZoneBindingInput{Upstream: "default", PowerDNSZoneID: "existing.test.", ZoneName: "existing.test."}
	existing, err := store1.EnsureZoneBinding(t.Context(), operator, existingInput)
	if err != nil || existing.Generation != 1 {
		t.Fatalf("EnsureZoneBinding(new existing zone) = %+v, %v", existing, err)
	}
	converged, err := store2.EnsureZoneBinding(t.Context(), operator2, existingInput)
	if err != nil || converged.ID != existing.ID {
		t.Fatalf("EnsureZoneBinding(repeat) = %+v, %v, want %s", converged, err, existing.ID)
	}
}
