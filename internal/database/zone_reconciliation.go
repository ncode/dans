package database

import (
	"context"
	"fmt"
	"time"

	"github.com/ncode/dans/internal/dnsname"
	"github.com/ncode/dans/internal/identifier"
)

type ZoneObservationInput struct {
	BindingID      string
	ZonePresent    bool
	ObservedZoneID *string
}

type ZoneObservation struct {
	BindingID      string
	ObservedAt     time.Time
	ZonePresent    bool
	ObservedZoneID *string
}

func (s *Store) ObserveZoneBinding(ctx context.Context, actor Actor, input ZoneObservationInput) (ZoneObservation, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneObservation{}, err
	}
	if identifier.ValidateUUID(input.BindingID) != nil ||
		(input.ZonePresent && (input.ObservedZoneID == nil || *input.ObservedZoneID == "" || len(*input.ObservedZoneID) > 255)) ||
		(!input.ZonePresent && input.ObservedZoneID != nil) {
		return ZoneObservation{}, ErrInvalid
	}
	if _, err := s.queries.GetZoneBindingByID(ctx, input.BindingID); err != nil {
		return ZoneObservation{}, fmt.Errorf("get observed zone binding: %w", mapStoreError(err))
	}
	var observedID *string
	if input.ObservedZoneID != nil {
		value := *input.ObservedZoneID
		observedID = &value
	}
	return ZoneObservation{
		BindingID: input.BindingID, ObservedAt: time.Now().UTC(),
		ZonePresent: input.ZonePresent, ObservedZoneID: observedID,
	}, nil
}

func (s *Store) ConfirmZoneBindingAbsent(ctx context.Context, actor Actor, bindingID string) (ZoneBinding, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneBinding{}, err
	}
	if identifier.ValidateUUID(bindingID) != nil {
		return ZoneBinding{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("begin confirm zone absence: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	binding, err := q.GetZoneBindingForUpdate(ctx, bindingID)
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("get zone binding for absence confirmation: %w", mapStoreError(err))
	}
	if !binding.RetiredAt.Valid {
		return ZoneBinding{}, ErrConflict
	}
	state, err := q.GetZoneDeletionReconciliationState(ctx, binding.ID)
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("get zone deletion reconciliation state: %w", err)
	}
	if !state.HasAttempt || state.HasSucceeded {
		return ZoneBinding{}, ErrConflict
	}
	if state.ConfirmedAbsent {
		return binding, nil
	}
	if err := recordManagementAudit(ctx, q, actor, "zone_binding.confirm_absent", "zone_binding", binding.ID, map[string]any{
		"upstream": binding.Upstream, "powerdns_zone_id": binding.PowerDNSZoneID, "zone_name": binding.ZoneName,
	}); err != nil {
		return ZoneBinding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ZoneBinding{}, fmt.Errorf("commit zone absence confirmation: %w", err)
	}
	return binding, nil
}

func (s *Store) PrepareZoneDeletionRetry(ctx context.Context, actor Actor, input ZoneDeletionInput) (ZoneDeletionPlan, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneDeletionPlan{}, err
	}
	if identifier.ValidateUUID(input.BindingID) != nil || len(input.RequestDigest) != 32 || !input.Deadline.After(time.Now()) {
		return ZoneDeletionPlan{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("begin retry zone deletion: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	binding, err := q.GetZoneBindingForUpdate(ctx, input.BindingID)
	if err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("get zone binding for deletion retry: %w", mapStoreError(err))
	}
	if !binding.RetiredAt.Valid {
		return ZoneDeletionPlan{}, ErrConflict
	}
	state, err := q.GetZoneDeletionReconciliationState(ctx, binding.ID)
	if err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("get zone deletion reconciliation state: %w", err)
	}
	if !state.HasRetryable || state.HasPending || state.HasSucceeded || state.ConfirmedAbsent {
		return ZoneDeletionPlan{}, ErrConflict
	}
	intent, err := createDNSIntentWithQueries(ctx, q, actor, DNSIntentInput{
		Action: "powerdns.zone.delete", TargetKind: "zone_binding", TargetID: binding.ID,
		RequestDigest: input.RequestDigest, Deadline: input.Deadline,
	})
	if err != nil {
		return ZoneDeletionPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("commit zone deletion retry: %w", err)
	}
	return ZoneDeletionPlan{
		Binding: binding, Intent: intent, Upstream: binding.Upstream,
		PowerDNSZoneID: binding.PowerDNSZoneID, ZoneName: binding.ZoneName,
	}, nil
}

func (s *Store) RebindZoneBinding(ctx context.Context, actor Actor, retiredBindingID string, input ZoneBindingInput) (ZoneBinding, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneBinding{}, err
	}
	if identifier.ValidateUUID(retiredBindingID) != nil || !validHandle(input.Upstream) || input.PowerDNSZoneID == "" || len(input.PowerDNSZoneID) > 255 {
		return ZoneBinding{}, ErrInvalid
	}
	zone, err := dnsname.Parse(input.ZoneName)
	if err != nil {
		return ZoneBinding{}, ErrInvalid
	}
	input.ZoneName = zone.String()
	retired, err := s.queries.GetZoneBindingByID(ctx, retiredBindingID)
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("get retired zone binding: %w", mapStoreError(err))
	}
	if !retired.RetiredAt.Valid || input.Upstream != retired.Upstream || input.ZoneName != retired.ZoneName {
		return ZoneBinding{}, ErrConflict
	}
	return s.ensureZoneBinding(ctx, actor, input, true)
}
