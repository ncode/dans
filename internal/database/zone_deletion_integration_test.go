//go:build integration

package database

import (
	"bytes"
	"errors"
	"testing"
	"time"

	delegationdomain "github.com/ncode/dans/internal/delegation"
)

func TestStorePrepareZoneDeletionRetiresAuthorityBeforeHandoff(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "239")
	identity := insertTestIdentity(t, conn, "240", "delete-writer")
	_, writerSecret := insertTestActorTokenWithSecret(t, conn, identity, "340")
	binding, err := store.BindCreatedZone(t.Context(), operator, ZoneBindingInput{
		Upstream: "default", PowerDNSZoneID: "delete.test.", ZoneName: "delete.test.",
	})
	if err != nil {
		t.Fatalf("BindCreatedZone() error = %v", err)
	}
	grant := createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorGlob, Value: "*.delete.test."}}, nil, nil)
	tuple := RRsetTuple{Owner: "host.delete.test.", RecordType: "A", ChangeKind: "REPLACE"}
	if decision := authorizeOne(t, store, writerSecret, "before-delete", "delete.test.", tuple); !decision.Allowed {
		t.Fatal("delegation was not effective before deletion preparation")
	}

	plan, err := store.PrepareZoneDeletion(t.Context(), operator, ZoneDeletionInput{
		BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x44}, 32),
		Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareZoneDeletion() error = %v", err)
	}
	if plan.Binding.ID != binding.ID || !plan.Binding.RetiredAt.Valid || plan.Intent.EventID == "" || plan.Upstream != "default" || plan.PowerDNSZoneID != "delete.test." {
		t.Errorf("PrepareZoneDeletion() = %+v", plan)
	}
	var bindingRetired, delegationRevoked, intentDurable bool
	if err := conn.QueryRow(t.Context(), `
		SELECT
			(SELECT retired_at IS NOT NULL FROM zone_bindings WHERE id = $1),
			(SELECT revoked_at IS NOT NULL FROM delegations WHERE id = $2),
			(SELECT count(*) = 1 FROM audit_events WHERE id = $3 AND event_kind = 'dns_intent')`,
		binding.ID, grant.ID, plan.Intent.EventID,
	).Scan(&bindingRetired, &delegationRevoked, &intentDurable); err != nil {
		t.Fatalf("read prepared deletion state: %v", err)
	}
	if !bindingRetired || !delegationRevoked || !intentDurable {
		t.Errorf("prepared deletion state = binding retired %t, grant revoked %t, intent durable %t", bindingRetired, delegationRevoked, intentDurable)
	}
	if decision := authorizeOne(t, store, writerSecret, "after-delete-prepare", "delete.test.", tuple); decision.Allowed {
		t.Fatal("retired binding still authorized after deletion preparation")
	}
	if _, err := store.PrepareZoneDeletion(t.Context(), operator, ZoneDeletionInput{
		BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x44}, 32), Deadline: time.Now().Add(time.Minute),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("PrepareZoneDeletion(repeat) error = %v, want %v", err, ErrConflict)
	}

	code := 404
	outcome, err := store.RecordZoneDeletionOutcome(t.Context(), plan, ZoneDeletionOutcomeInput{
		Result: ZoneDeletionNotFound, ResponseClass: "not_found", ResponseCode: &code,
		ResponseDigest: bytes.Repeat([]byte{0x55}, 32),
	})
	if err != nil || outcome.Result != "succeeded" {
		t.Fatalf("RecordZoneDeletionOutcome(not found) = %+v, %v", outcome, err)
	}
	stillRetired, err := store.GetZoneBinding(t.Context(), operator, binding.ID)
	if err != nil || !stillRetired.RetiredAt.Valid {
		t.Fatalf("binding after completed deletion = %+v, %v", stillRetired, err)
	}
}

func TestStorePrepareZoneDeletionFailsClosedWhenAuditUnavailable(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "241")
	identity := insertTestIdentity(t, conn, "242", "delete-audit-writer")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "412", "audit-delete.test.")
	grant := createIdentityGrant(t, store, operator, binding.ID, identity.ID,
		[]DelegationSelectorInput{{Kind: delegationdomain.SelectorExact, Value: "host.audit-delete.test."}}, nil, nil)
	installRejectAuditTrigger(t, conn)

	if _, err := store.PrepareZoneDeletion(t.Context(), operator, ZoneDeletionInput{
		BindingID: binding.ID, RequestDigest: bytes.Repeat([]byte{0x66}, 32), Deadline: time.Now().Add(time.Minute),
	}); !errors.Is(err, ErrAuditUnavailable) {
		t.Fatalf("PrepareZoneDeletion(audit unavailable) error = %v, want %v", err, ErrAuditUnavailable)
	}
	var activeBinding, activeGrant bool
	if err := conn.QueryRow(t.Context(), `
		SELECT
			(SELECT retired_at IS NULL FROM zone_bindings WHERE id = $1),
			(SELECT revoked_at IS NULL FROM delegations WHERE id = $2)`, binding.ID, grant.ID).Scan(&activeBinding, &activeGrant); err != nil {
		t.Fatalf("read rolled-back deletion preparation: %v", err)
	}
	if !activeBinding || !activeGrant {
		t.Errorf("audit failure did not preserve authority: binding active %t, grant active %t", activeBinding, activeGrant)
	}
}

func TestStorePrepareZoneDeletionByUpstreamZoneID(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "243")
	binding := insertTestZoneBinding(t, conn, operator.IdentityID, "413", "route-delete.test.")

	plan, err := store.PrepareZoneDeletionByUpstreamZoneID(t.Context(), operator, ZoneDeletionByUpstreamInput{
		Upstream: binding.Upstream, PowerDNSZoneID: binding.PowerDNSZoneID,
		RequestDigest: bytes.Repeat([]byte{0x77}, 32), Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareZoneDeletionByUpstreamZoneID() error = %v", err)
	}
	if plan.Binding.ID != binding.ID || !plan.Binding.RetiredAt.Valid || plan.Intent.EventID == "" {
		t.Errorf("PrepareZoneDeletionByUpstreamZoneID() = %+v", plan)
	}
	code := 500
	if _, err := store.RecordZoneDeletionOutcome(t.Context(), plan, ZoneDeletionOutcomeInput{
		Result: ZoneDeletionFailed, ResponseClass: "server_error", ResponseCode: &code,
	}); err != nil {
		t.Fatalf("RecordZoneDeletionOutcome(failed) error = %v", err)
	}
	if _, err := store.PrepareZoneDeletionByUpstreamZoneID(t.Context(), operator, ZoneDeletionByUpstreamInput{
		Upstream: binding.Upstream, PowerDNSZoneID: binding.PowerDNSZoneID,
		RequestDigest: bytes.Repeat([]byte{0x79}, 32), Deadline: time.Now().Add(time.Minute),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("PrepareZoneDeletionByUpstreamZoneID(retired history) error = %v, want %v", err, ErrConflict)
	}
	var intentCount int
	if err := conn.QueryRow(t.Context(), `
		SELECT count(*) FROM audit_events
		WHERE event_kind = 'dns_intent' AND action = 'powerdns.zone.delete'`,
	).Scan(&intentCount); err != nil {
		t.Fatalf("count deletion intents: %v", err)
	}
	if intentCount != 1 {
		t.Errorf("deletion intent count = %d, want 1", intentCount)
	}
}

func TestStorePrepareZoneDeletionByUpstreamZoneIDAllowsUnboundPowerDNSZone(t *testing.T) {
	t.Parallel()

	conn, _, _ := newTestSchema(t)
	if err := Migrate(t.Context(), conn); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := NewStore(conn)
	operator := insertTestOperator(t, conn, "244")
	plan, err := store.PrepareZoneDeletionByUpstreamZoneID(t.Context(), operator, ZoneDeletionByUpstreamInput{
		Upstream: "default", PowerDNSZoneID: "unbound-delete.test.",
		RequestDigest: bytes.Repeat([]byte{0x78}, 32), Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareZoneDeletionByUpstreamZoneID(unbound) error = %v", err)
	}
	if plan.Binding.ID != "" || plan.Intent.EventID == "" || plan.Upstream != "default" || plan.PowerDNSZoneID != "unbound-delete.test." {
		t.Fatalf("unbound deletion plan = %+v", plan)
	}
	var targetKind, targetID string
	if err := conn.QueryRow(t.Context(), `SELECT target_kind, target_id FROM audit_events WHERE id = $1`, plan.Intent.EventID).Scan(&targetKind, &targetID); err != nil {
		t.Fatalf("read unbound deletion intent: %v", err)
	}
	if targetKind != "powerdns_zone" || targetID != "default:unbound-delete.test." {
		t.Errorf("intent target = %s %s", targetKind, targetID)
	}
}
