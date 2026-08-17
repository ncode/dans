package database

import (
	"context"
	"fmt"
	"time"

	"github.com/ncode/dans/internal/identifier"
)

type ZoneDeletionInput struct {
	BindingID     string
	RequestDigest []byte
	Deadline      time.Time
}

type ZoneDeletionByUpstreamInput struct {
	Upstream       string
	PowerDNSZoneID string
	RequestDigest  []byte
	Deadline       time.Time
}

type ZoneDeletionPlan struct {
	Binding        ZoneBinding
	Intent         DNSIntent
	Upstream       string
	PowerDNSZoneID string
	ZoneName       string
}

type ZoneDeletionResult string

const (
	ZoneDeletionDeleted  ZoneDeletionResult = "deleted"
	ZoneDeletionNotFound ZoneDeletionResult = "not_found"
	ZoneDeletionFailed   ZoneDeletionResult = "failed"
	ZoneDeletionUnknown  ZoneDeletionResult = "unknown"
)

type ZoneDeletionOutcomeInput struct {
	Result         ZoneDeletionResult
	ResponseClass  string
	ResponseCode   *int
	ResponseDigest []byte
}

func (s *Store) PrepareZoneDeletion(ctx context.Context, actor Actor, input ZoneDeletionInput) (ZoneDeletionPlan, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneDeletionPlan{}, err
	}
	if identifier.ValidateUUID(input.BindingID) != nil || len(input.RequestDigest) != 32 || !input.Deadline.After(time.Now()) {
		return ZoneDeletionPlan{}, ErrInvalid
	}
	return s.prepareZoneDeletion(ctx, actor, input.RequestDigest, input.Deadline, func(q *Queries) (ZoneBinding, error) {
		return q.GetZoneBindingForUpdate(ctx, input.BindingID)
	})
}

func (s *Store) PrepareZoneDeletionByUpstreamZoneID(ctx context.Context, actor Actor, input ZoneDeletionByUpstreamInput) (ZoneDeletionPlan, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneDeletionPlan{}, err
	}
	if !validHandle(input.Upstream) || input.PowerDNSZoneID == "" || len(input.PowerDNSZoneID) > 255 || len(input.RequestDigest) != 32 || !input.Deadline.After(time.Now()) {
		return ZoneDeletionPlan{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("begin prepare zone deletion: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	if err := q.LockZoneBindingKey(ctx, "id:"+input.Upstream+":"+input.PowerDNSZoneID); err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("lock zone deletion key: %w", err)
	}
	binding, err := q.GetActiveZoneBindingByUpstreamZoneIDForUpdate(ctx, GetActiveZoneBindingByUpstreamZoneIDForUpdateParams{
		Upstream: input.Upstream, PowerDNSZoneID: input.PowerDNSZoneID,
	})
	if err != nil && mapStoreError(err) != ErrNotFound {
		return ZoneDeletionPlan{}, fmt.Errorf("get zone binding for deletion: %w", mapStoreError(err))
	}
	if mapStoreError(err) == ErrNotFound {
		history, err := q.ZoneBindingHistoryExists(ctx, ZoneBindingHistoryExistsParams{
			Upstream: input.Upstream, PowerDNSZoneID: input.PowerDNSZoneID,
		})
		if err != nil {
			return ZoneDeletionPlan{}, fmt.Errorf("check zone binding history for deletion: %w", err)
		}
		if history {
			return ZoneDeletionPlan{}, ErrConflict
		}
		intent, err := createDNSIntentWithQueries(ctx, q, actor, DNSIntentInput{
			Action: "powerdns.zone.delete", TargetKind: "powerdns_zone",
			TargetID:      input.Upstream + ":" + input.PowerDNSZoneID,
			RequestDigest: input.RequestDigest, Deadline: input.Deadline,
		})
		if err != nil {
			return ZoneDeletionPlan{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ZoneDeletionPlan{}, fmt.Errorf("commit unbound zone deletion preparation: %w", err)
		}
		return ZoneDeletionPlan{Intent: intent, Upstream: input.Upstream, PowerDNSZoneID: input.PowerDNSZoneID}, nil
	}
	plan, err := prepareBoundZoneDeletion(ctx, q, actor, binding, input.RequestDigest, input.Deadline)
	if err != nil {
		return ZoneDeletionPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("commit zone deletion preparation: %w", err)
	}
	return plan, nil
}

func (s *Store) prepareZoneDeletion(ctx context.Context, actor Actor, requestDigest []byte, deadline time.Time, findBinding func(*Queries) (ZoneBinding, error)) (ZoneDeletionPlan, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("begin prepare zone deletion: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	binding, err := findBinding(q)
	if err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("get zone binding for deletion: %w", mapStoreError(err))
	}
	plan, err := prepareBoundZoneDeletion(ctx, q, actor, binding, requestDigest, deadline)
	if err != nil {
		return ZoneDeletionPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("commit zone deletion preparation: %w", err)
	}
	return plan, nil
}

func prepareBoundZoneDeletion(ctx context.Context, q *Queries, actor Actor, binding ZoneBinding, requestDigest []byte, deadline time.Time) (ZoneDeletionPlan, error) {
	if binding.RetiredAt.Valid {
		return ZoneDeletionPlan{}, ErrConflict
	}
	revokedIDs, err := q.RevokeDelegationsForZoneDeletion(ctx, RevokeDelegationsForZoneDeletionParams{
		RevokedByIdentityID: actor.IdentityID, ZoneBindingID: binding.ID,
	})
	if err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("revoke zone delegations: %w", err)
	}
	retired, err := q.RetireZoneBinding(ctx, RetireZoneBindingParams{ID: binding.ID, RetiredByIdentityID: actor.IdentityID})
	if err != nil {
		return ZoneDeletionPlan{}, fmt.Errorf("retire zone binding: %w", mapStoreError(err))
	}
	if err := recordManagementAudit(ctx, q, actor, "zone_binding.retire", "zone_binding", binding.ID, map[string]any{
		"upstream": binding.Upstream, "powerdns_zone_id": binding.PowerDNSZoneID,
		"zone_name": binding.ZoneName, "revoked_delegation_count": len(revokedIDs),
	}); err != nil {
		return ZoneDeletionPlan{}, err
	}
	intent, err := createDNSIntentWithQueries(ctx, q, actor, DNSIntentInput{
		Action: "powerdns.zone.delete", TargetKind: "zone_binding", TargetID: binding.ID,
		RequestDigest: requestDigest, Deadline: deadline,
	})
	if err != nil {
		return ZoneDeletionPlan{}, err
	}
	return ZoneDeletionPlan{
		Binding: retired, Intent: intent, Upstream: binding.Upstream,
		PowerDNSZoneID: binding.PowerDNSZoneID, ZoneName: binding.ZoneName,
	}, nil
}

func (s *Store) RecordZoneDeletionOutcome(ctx context.Context, plan ZoneDeletionPlan, input ZoneDeletionOutcomeInput) (DNSOutcome, error) {
	result := ""
	switch input.Result {
	case ZoneDeletionDeleted, ZoneDeletionNotFound:
		result = "succeeded"
	case ZoneDeletionFailed:
		result = "failed"
	case ZoneDeletionUnknown:
		result = "unknown"
	default:
		return DNSOutcome{}, ErrInvalid
	}
	return s.RecordDNSOutcome(ctx, DNSOutcomeInput{
		IntentEventID: plan.Intent.EventID, OperationID: plan.Intent.OperationID,
		Result: result, ResponseClass: input.ResponseClass,
		ResponseCode: input.ResponseCode, ResponseDigest: input.ResponseDigest,
	})
}
