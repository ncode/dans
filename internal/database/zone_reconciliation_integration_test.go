//go:build integration

package database

import (
	"bytes"
	"errors"
	"testing"
	"time"

	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestStoreZoneReconciliationNeverReactivatesOrCopiesAuthority(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "244")
	identity := insertTestIdentity(t, conn, "245", "reconcile-writer")
	_, writerSecret := insertTestActorTokenWithSecret(t, conn, identity, "345")
	binding, err := store.BindCreatedZone(t.Context(), operator, ZoneBindingInput{
		Upstream: "default", PowerDNSZoneID: "old-reconcile-id", ZoneName: "reconcile.test.",
	})
	if err != nil {
		t.Fatalf("BindCreatedZone() error = %v", err)
	}
	grant := createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorGlob, Value: "*.reconcile.test."}}, nil, nil)
	plan, err := store.PrepareZoneDeletion(t.Context(), operator, ZoneDeletionInput{
		BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x81}, 32), Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareZoneDeletion() error = %v", err)
	}
	code := 500
	if _, err := store.RecordZoneDeletionOutcome(t.Context(), plan, ZoneDeletionOutcomeInput{
		Result: ZoneDeletionFailed, ResponseClass: "server_error", ResponseCode: &code,
		ResponseDigest: bytes.Repeat([]byte{0x82}, 32),
	}); err != nil {
		t.Fatalf("RecordZoneDeletionOutcome() error = %v", err)
	}

	observedID := binding.PowerDNSZoneID
	observation, err := store.ObserveZoneBinding(t.Context(), operator, ZoneObservationInput{
		BindingID: binding.ID, ZonePresent: true, ObservedZoneID: &observedID,
	})
	if err != nil {
		t.Fatalf("ObserveZoneBinding() error = %v", err)
	}
	if observation.BindingID != binding.ID || !observation.ZonePresent || observation.ObservedZoneID == nil || *observation.ObservedZoneID != observedID || observation.ObservedAt.IsZero() {
		t.Errorf("ObserveZoneBinding() = %+v", observation)
	}
	stillRetired, err := store.GetZoneBinding(t.Context(), operator, binding.ID)
	if err != nil || !stillRetired.RetiredAt.Valid {
		t.Fatalf("binding after observation = %+v, %v", stillRetired, err)
	}

	retry, err := store.PrepareZoneDeletionRetry(t.Context(), operator, ZoneDeletionInput{
		BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x83}, 32), Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareZoneDeletionRetry() error = %v", err)
	}
	if retry.Binding.ID != binding.ID || !retry.Binding.RetiredAt.Valid || retry.Intent.EventID == plan.Intent.EventID || retry.Intent.OperationID == plan.Intent.OperationID {
		t.Errorf("PrepareZoneDeletionRetry() = %+v", retry)
	}
	if _, err := store.RecordZoneDeletionOutcome(t.Context(), retry, ZoneDeletionOutcomeInput{Result: ZoneDeletionUnknown}); err != nil {
		t.Fatalf("RecordZoneDeletionOutcome(retry) error = %v", err)
	}

	rebound, err := store.RebindZoneBinding(t.Context(), operator, binding.ID, ZoneBindingInput{
		Upstream: binding.Upstream, PowerDNSZoneID: "new-reconcile-id", ZoneName: binding.ZoneName,
	})
	if err != nil {
		t.Fatalf("RebindZoneBinding() error = %v", err)
	}
	if rebound.ID == binding.ID || rebound.Generation <= binding.Generation || rebound.RetiredAt.Valid {
		t.Errorf("RebindZoneBinding() = %+v, old = %+v", rebound, binding)
	}
	var oldRevoked, copiedGrant bool
	if err := conn.QueryRow(t.Context(), `
		SELECT
			(SELECT revoked_at IS NOT NULL FROM delegations WHERE id = $1),
			EXISTS (SELECT 1 FROM delegations WHERE zone_binding_id = $2)`, grant.ID, rebound.ID,
	).Scan(&oldRevoked, &copiedGrant); err != nil {
		t.Fatalf("read rebound authority: %v", err)
	}
	if !oldRevoked || copiedGrant {
		t.Errorf("rebound authority = old revoked %t, copied grant %t", oldRevoked, copiedGrant)
	}
	tuple := RRsetTuple{Owner: "host.reconcile.test.", RecordType: "A", ChangeKind: "REPLACE"}
	if decision := authorizeOne(t, store, writerSecret, "after-rebind", rebound.PowerDNSZoneID, tuple); decision.Allowed {
		t.Fatal("recreated zone inherited authority from its retired binding")
	}
}

func TestStoreConfirmZoneBindingAbsentIsIdempotentAndEndsRetry(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "246")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "414", "confirm-absent.test.")
	plan, err := store.PrepareZoneDeletion(t.Context(), operator, ZoneDeletionInput{
		BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x84}, 32), Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareZoneDeletion() error = %v", err)
	}
	if _, err := store.PrepareZoneDeletionRetry(t.Context(), operator, ZoneDeletionInput{
		BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x85}, 32), Deadline: time.Now().Add(time.Minute),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("PrepareZoneDeletionRetry(pending) error = %v, want %v", err, ErrConflict)
	}
	if _, err := store.RecordZoneDeletionOutcome(t.Context(), plan, ZoneDeletionOutcomeInput{Result: ZoneDeletionUnknown}); err != nil {
		t.Fatalf("RecordZoneDeletionOutcome() error = %v", err)
	}

	confirmed, err := store.ConfirmZoneBindingAbsent(t.Context(), operator, binding.ID)
	if err != nil || !confirmed.RetiredAt.Valid {
		t.Fatalf("ConfirmZoneBindingAbsent() = %+v, %v", confirmed, err)
	}
	if _, err := store.ConfirmZoneBindingAbsent(t.Context(), operator, binding.ID); err != nil {
		t.Fatalf("ConfirmZoneBindingAbsent(repeat) error = %v", err)
	}
	var confirmations int
	if err := conn.QueryRow(t.Context(), `
		SELECT count(*) FROM audit_events
		WHERE action = 'zone_binding.confirm_absent' AND target_id = $1`, binding.ID,
	).Scan(&confirmations); err != nil {
		t.Fatalf("count absence confirmations: %v", err)
	}
	if confirmations != 1 {
		t.Errorf("absence confirmation event count = %d, want 1", confirmations)
	}
	if _, err := store.PrepareZoneDeletionRetry(t.Context(), operator, ZoneDeletionInput{
		BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x86}, 32), Deadline: time.Now().Add(time.Minute),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("PrepareZoneDeletionRetry(confirmed absent) error = %v, want %v", err, ErrConflict)
	}
}
