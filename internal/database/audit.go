package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ncode/dans/internal/dnsname"
	"github.com/ncode/dans/internal/identifier"
	"github.com/ncode/dans/internal/page"
)

type DNSIntentInput struct {
	Action               string
	TargetKind           string
	TargetID             string
	RRsets               []RRsetTuple
	MatchedDelegationIDs []string
	RequestDigest        []byte
	Deadline             time.Time
}

type DNSIntent struct {
	EventID     string
	OperationID string
	Deadline    time.Time
}

type DNSOutcomeInput struct {
	IntentEventID  string
	OperationID    string
	Result         string
	ResponseClass  string
	ResponseCode   *int
	ResponseDigest []byte
}

type DNSOutcome struct {
	ID             string
	EventKind      string
	IntentEventID  string
	OperationID    string
	Result         string
	ResponseClass  *string
	ResponseCode   *int32
	ResponseDigest []byte
	OccurredAt     pgtype.Timestamptz
}

type AuditListOptions struct {
	Limit      int
	After      *page.Key
	ActorID    *string
	Action     *string
	TargetKind *string
	TargetID   *string
	Result     *string
}

type AuditRecord struct {
	ID         string          `json:"id"`
	OccurredAt time.Time       `json:"occurred_at"`
	RequestID  string          `json:"request_id"`
	ActorID    *string         `json:"actor_id"`
	Action     string          `json:"action"`
	TargetKind string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Result     string          `json:"result"`
	Details    json.RawMessage `json:"details"`
}

type AuditPage struct {
	Items []AuditRecord
	Next  *page.Key
}

func (s *Store) CreateDNSIntent(ctx context.Context, actor Actor, input DNSIntentInput) (DNSIntent, error) {
	return createDNSIntentWithQueries(ctx, s.queries, actor, input)
}

// ProbeAuditPersistence verifies the complete audit INSERT path without
// retaining a probe event in the immutable ledger.
func (s *Store) ProbeAuditPersistence(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin audit persistence probe: %v", ErrAuditUnavailable, err)
	}
	defer rollback(tx)
	if err := recordManagementAudit(ctx, New(tx), Actor{RequestID: "readiness-probe"},
		"system.audit_probe", "audit_storage", "audit_events", map[string]any{}); err != nil {
		return err
	}
	if err := tx.Rollback(ctx); err != nil {
		return fmt.Errorf("%w: roll back audit persistence probe: %v", ErrAuditUnavailable, err)
	}
	return nil
}

func createDNSIntentWithQueries(ctx context.Context, q *Queries, actor Actor, input DNSIntentInput) (DNSIntent, error) {
	if err := requireActor(actor); err != nil {
		return DNSIntent{}, err
	}
	if input.Action == "" || input.TargetKind == "" || input.TargetID == "" || len(input.RequestDigest) != 32 || !input.Deadline.After(time.Now()) || len(input.RRsets) > MaxRRsetBatch {
		return DNSIntent{}, ErrInvalid
	}
	summary := make([]RRsetTuple, len(input.RRsets))
	for i, tuple := range input.RRsets {
		owner, err := dnsname.Parse(tuple.Owner)
		if err != nil || !validUniqueRecordTypes([]string{tuple.RecordType}) || !validUniqueChangeKinds([]string{tuple.ChangeKind}) {
			return DNSIntent{}, ErrInvalid
		}
		summary[i] = RRsetTuple{Owner: owner.String(), RecordType: tuple.RecordType, ChangeKind: tuple.ChangeKind}
	}
	matched := slices.Clone(input.MatchedDelegationIDs)
	slices.Sort(matched)
	matched = slices.Compact(matched)
	for _, id := range matched {
		if identifier.ValidateUUID(id) != nil {
			return DNSIntent{}, ErrInvalid
		}
	}
	rrsetSummary, err := json.Marshal(summary)
	if err != nil {
		return DNSIntent{}, fmt.Errorf("encode RRset audit summary: %w", err)
	}
	eventID, err := identifier.NewUUID()
	if err != nil {
		return DNSIntent{}, fmt.Errorf("generate DNS intent event id: %w", err)
	}
	operationID, err := identifier.NewUUID()
	if err != nil {
		return DNSIntent{}, fmt.Errorf("generate DNS operation id: %w", err)
	}
	_, err = q.InsertDNSIntent(ctx, InsertDNSIntentParams{
		ID: eventID, OperationID: operationID, RequestID: actor.RequestID,
		ActorIdentityID: actor.IdentityID, ActorTokenID: actor.TokenID,
		Action: input.Action, TargetKind: input.TargetKind, TargetID: input.TargetID,
		RRsetSummary: rrsetSummary, MatchedDelegationIDs: matched,
		RequestDigest: slices.Clone(input.RequestDigest),
		DeadlineAt:    pgtype.Timestamptz{Time: input.Deadline.UTC(), Valid: true},
	})
	if err != nil {
		return DNSIntent{}, fmt.Errorf("%w: insert DNS mutation intent: %v", ErrAuditUnavailable, err)
	}
	return DNSIntent{EventID: eventID, OperationID: operationID, Deadline: input.Deadline.UTC()}, nil
}

func (s *Store) RecordDNSOutcome(ctx context.Context, input DNSOutcomeInput) (DNSOutcome, error) {
	if identifier.ValidateUUID(input.IntentEventID) != nil || identifier.ValidateUUID(input.OperationID) != nil || !validDNSOutcome(input) {
		return DNSOutcome{}, ErrInvalid
	}
	eventID, err := identifier.NewUUID()
	if err != nil {
		return DNSOutcome{}, fmt.Errorf("generate DNS outcome event id: %w", err)
	}
	var responseCode *int32
	if input.ResponseCode != nil {
		value := int32(*input.ResponseCode)
		responseCode = &value
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return DNSOutcome{}, fmt.Errorf("begin DNS outcome: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	_, err = q.InsertDNSOutcome(ctx, InsertDNSOutcomeParams{
		ID: eventID, Result: input.Result, ResponseClass: input.ResponseClass,
		ResponseCode: responseCode, ResponseDigest: slices.Clone(input.ResponseDigest),
		IntentEventID: input.IntentEventID, OperationID: input.OperationID,
	})
	if err != nil {
		return DNSOutcome{}, fmt.Errorf("%w: insert DNS mutation outcome: %v", ErrAuditUnavailable, err)
	}
	row, err := q.GetDNSOutcomeByOperationID(ctx, input.OperationID)
	if err != nil {
		return DNSOutcome{}, fmt.Errorf("get DNS mutation outcome: %w", mapStoreError(err))
	}
	outcome := DNSOutcome{
		ID: row.ID, EventKind: row.EventKind, IntentEventID: row.IntentEventID,
		OperationID: row.OperationID, Result: row.Result, ResponseClass: row.ResponseClass,
		ResponseCode: row.ResponseCode, ResponseDigest: row.ResponseDigest, OccurredAt: row.OccurredAt,
	}
	if !sameDNSOutcome(outcome, input, responseCode) {
		return DNSOutcome{}, ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return DNSOutcome{}, fmt.Errorf("%w: commit DNS mutation outcome: %v", ErrAuditUnavailable, err)
	}
	return outcome, nil
}

func (s *Store) CloseOverdueDNSIntents(ctx context.Context, requestedLimit int) (int, error) {
	limit, err := page.NormalizeLimit(requestedLimit)
	if err != nil {
		return 0, ErrInvalid
	}
	eventIDs := make([]string, limit)
	for i := range eventIDs {
		eventIDs[i], err = identifier.NewUUID()
		if err != nil {
			return 0, fmt.Errorf("generate overdue outcome event id: %w", err)
		}
	}
	ids, err := s.queries.CloseOverdueDNSIntents(ctx, CloseOverdueDNSIntentsParams{
		RowLimit: int32(limit), EventIDs: eventIDs,
	})
	if err != nil {
		return 0, fmt.Errorf("%w: close overdue DNS intents: %v", ErrAuditUnavailable, err)
	}
	return len(ids), nil
}

func (s *Store) ListAuditEvents(ctx context.Context, actor Actor, options AuditListOptions) (AuditPage, error) {
	if err := requireOperator(actor); err != nil {
		return AuditPage{}, err
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return AuditPage{}, ErrInvalid
	}
	params := ListAuditRecordsParams{RowLimit: int32(limit + 1)}
	if options.ActorID != nil {
		if identifier.ValidateUUID(*options.ActorID) != nil {
			return AuditPage{}, ErrInvalid
		}
		params.ActorID = *options.ActorID
	}
	for _, filter := range []struct {
		value       *string
		destination *string
	}{
		{options.Action, &params.Action},
		{options.TargetKind, &params.TargetKind},
		{options.TargetID, &params.TargetID},
		{options.Result, &params.Result},
	} {
		if filter.value != nil {
			if *filter.value == "" {
				return AuditPage{}, ErrInvalid
			}
			*filter.destination = *filter.value
		}
	}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return AuditPage{}, ErrInvalid
		}
		params.AfterOccurredAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	rows, err := s.queries.ListAuditRecords(ctx, params)
	if err != nil {
		return AuditPage{}, fmt.Errorf("list audit events: %w", err)
	}
	items := make([]AuditRecord, len(rows))
	for i, row := range rows {
		var actorID *string
		if row.ActorID != "" {
			value := row.ActorID
			actorID = &value
		}
		items[i] = AuditRecord{
			ID: row.ID, OccurredAt: row.OccurredAt.Time, RequestID: row.RequestID,
			ActorID: actorID, Action: row.Action, TargetKind: row.TargetKind,
			TargetID: row.TargetID, Result: row.Result, Details: slices.Clone(row.Details),
		}
	}
	result := AuditPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.OccurredAt, ID: last.ID}
	}
	return result, nil
}

func (s *Store) WriteAuditNDJSON(ctx context.Context, actor Actor, options AuditListOptions, output io.Writer) error {
	if output == nil {
		return ErrInvalid
	}
	encoder := json.NewEncoder(output)
	for {
		result, err := s.ListAuditEvents(ctx, actor, options)
		if err != nil {
			return err
		}
		for _, event := range result.Items {
			if err := encoder.Encode(event); err != nil {
				return fmt.Errorf("write audit NDJSON: %w", err)
			}
		}
		if result.Next == nil {
			return nil
		}
		options.After = result.Next
	}
}

func validDNSOutcome(input DNSOutcomeInput) bool {
	switch input.Result {
	case "succeeded", "failed", "unknown":
	default:
		return false
	}
	if input.ResponseCode != nil && (*input.ResponseCode < 100 || *input.ResponseCode > 599) {
		return false
	}
	return len(input.ResponseDigest) == 0 || len(input.ResponseDigest) == 32
}

func sameDNSOutcome(outcome DNSOutcome, input DNSOutcomeInput, responseCode *int32) bool {
	if outcome.IntentEventID != input.IntentEventID || outcome.OperationID != input.OperationID || outcome.Result != input.Result || !bytes.Equal(outcome.ResponseDigest, input.ResponseDigest) {
		return false
	}
	if (outcome.ResponseClass == nil) != (input.ResponseClass == "") || outcome.ResponseClass != nil && *outcome.ResponseClass != input.ResponseClass {
		return false
	}
	if (outcome.ResponseCode == nil) != (responseCode == nil) || outcome.ResponseCode != nil && *outcome.ResponseCode != *responseCode {
		return false
	}
	return true
}
