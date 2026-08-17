package database

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ncode/dans/internal/dnsname"
	"github.com/ncode/dans/internal/identifier"
)

const MaxRRsetBatch = 100

type RRsetTuple struct {
	Owner      string
	RecordType string
	ChangeKind string
}

type DeniedRRsetTuple struct {
	Index      int    `json:"index"`
	Owner      string `json:"owner"`
	RecordType string `json:"record_type"`
	ChangeKind string `json:"change_kind"`
}

type AuthorizationDecision struct {
	Actor                Actor              `json:"-"`
	Allowed              bool               `json:"allowed"`
	ZoneBindingID        string             `json:"-"`
	Denied               []DeniedRRsetTuple `json:"denied"`
	MatchedDelegationIDs []string           `json:"-"`
}

// AuthorizationDenialInput is sanitized evidence for an authenticated request
// rejected before a DNS mutation handler is reached.
type AuthorizationDenialInput struct {
	Action        string
	TargetKind    string
	TargetID      string
	Details       map[string]any
	RequestDigest []byte
}

type authorizationTuple struct {
	Owner           string `json:"owner"`
	RecordType      string `json:"record_type"`
	ChangeKind      string `json:"change_kind"`
	LiteralWildcard bool   `json:"literal_wildcard"`
}

// AuthorizeRRsetBatch evaluates every tuple against one current database statement.
func (s *Store) AuthorizeRRsetBatch(ctx context.Context, token, requestID, upstream, powerDNSZoneID string, tuples []RRsetTuple) (AuthorizationDecision, error) {
	if identifier.ValidateToken(token) != nil {
		return AuthorizationDecision{}, ErrUnauthenticated
	}
	if requestID == "" || len(requestID) > 128 || upstream == "" || powerDNSZoneID == "" || len(tuples) == 0 || len(tuples) > MaxRRsetBatch {
		return AuthorizationDecision{}, ErrInvalid
	}
	canonical := make([]authorizationTuple, len(tuples))
	for i, tuple := range tuples {
		owner, err := dnsname.Parse(tuple.Owner)
		if err != nil || !validUniqueRecordTypes([]string{tuple.RecordType}) || !validUniqueChangeKinds([]string{tuple.ChangeKind}) {
			return AuthorizationDecision{}, ErrInvalid
		}
		canonical[i] = authorizationTuple{
			Owner: owner.String(), RecordType: tuple.RecordType, ChangeKind: tuple.ChangeKind,
			LiteralWildcard: owner.HasLiteralWildcard(),
		}
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("encode authorization tuples: %w", err)
	}
	digest := identifier.DigestToken(token)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("begin RRset authorization: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	rows, err := q.AuthorizeRRsetBatch(ctx, AuthorizeRRsetBatchParams{
		TokenDigest: digest[:], Tuples: encoded, Upstream: upstream, PowerDNSZoneID: powerDNSZoneID,
	})
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("authorize RRset batch: %w", err)
	}
	if len(rows) != len(tuples) {
		return AuthorizationDecision{}, fmt.Errorf("authorize RRset batch: database returned %d tuple decisions, want %d", len(rows), len(tuples))
	}
	if len(rows) == 0 || rows[0].TokenID == "" {
		return AuthorizationDecision{}, ErrUnauthenticated
	}
	decision := AuthorizationDecision{
		Actor: Actor{
			IdentityID: rows[0].IdentityID, TokenID: rows[0].TokenID,
			Kind: rows[0].IdentityKind, Handle: rows[0].IdentityHandle,
			Operator: rows[0].IsOperator, RequestID: requestID,
		},
		Allowed:       true,
		ZoneBindingID: rows[0].ZoneBindingID,
	}
	matched := make(map[string]struct{})
	for i, row := range rows {
		if int(row.BatchIndex) != i {
			return AuthorizationDecision{}, fmt.Errorf("authorize RRset batch: database returned tuple index %d at position %d", row.BatchIndex, i)
		}
		if !row.Allowed {
			decision.Allowed = false
			decision.Denied = append(decision.Denied, DeniedRRsetTuple{
				Index: i, Owner: tuples[i].Owner, RecordType: tuples[i].RecordType, ChangeKind: tuples[i].ChangeKind,
			})
			continue
		}
		for _, id := range row.MatchedDelegationIDs {
			matched[id] = struct{}{}
		}
	}
	if !decision.Allowed {
		if err := recordAuthorizationDenial(ctx, q, decision, encoded, upstream, powerDNSZoneID); err != nil {
			return decision, err
		}
		if err := tx.Commit(ctx); err != nil {
			return decision, fmt.Errorf("%w: commit authorization denial: %v", ErrAuditUnavailable, err)
		}
		return decision, nil
	}
	decision.MatchedDelegationIDs = make([]string, 0, len(matched))
	for id := range matched {
		decision.MatchedDelegationIDs = append(decision.MatchedDelegationIDs, id)
	}
	slices.Sort(decision.MatchedDelegationIDs)
	if err := tx.Commit(ctx); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("commit RRset authorization: %w", err)
	}
	return decision, nil
}

// RecordAuthorizationDenial appends one durable, sanitized denial event.
func (s *Store) RecordAuthorizationDenial(ctx context.Context, actor Actor, input AuthorizationDenialInput) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	if input.Action == "" || input.TargetKind == "" || input.TargetID == "" || len(input.TargetID) > 1024 || len(input.RequestDigest) != sha256.Size {
		return ErrInvalid
	}
	details, err := json.Marshal(input.Details)
	if err != nil {
		return fmt.Errorf("encode authorization denial details: %w", err)
	}
	id, err := identifier.NewUUID()
	if err != nil {
		return fmt.Errorf("generate authorization denial audit id: %w", err)
	}
	_, err = s.queries.InsertAuthorizationDeniedAudit(ctx, InsertAuthorizationDeniedAuditParams{
		ID: id, RequestID: actor.RequestID,
		ActorIdentityID: actor.IdentityID, ActorTokenID: actor.TokenID,
		Action: input.Action, TargetKind: input.TargetKind, TargetID: input.TargetID,
		Details: details, RequestDigest: slices.Clone(input.RequestDigest),
	})
	if err != nil {
		return fmt.Errorf("%w: insert authorization denial audit: %v", ErrAuditUnavailable, err)
	}
	return nil
}

func recordAuthorizationDenial(ctx context.Context, q *Queries, decision AuthorizationDecision, canonicalRequest []byte, upstream, powerDNSZoneID string) error {
	id, err := identifier.NewUUID()
	if err != nil {
		return fmt.Errorf("generate authorization denial audit id: %w", err)
	}
	details, err := json.Marshal(map[string]any{"denied": decision.Denied})
	if err != nil {
		return fmt.Errorf("encode authorization denial details: %w", err)
	}
	digest := sha256.Sum256(canonicalRequest)
	_, err = q.InsertAuthorizationDeniedAudit(ctx, InsertAuthorizationDeniedAuditParams{
		ID: id, RequestID: decision.Actor.RequestID,
		ActorIdentityID: decision.Actor.IdentityID, ActorTokenID: decision.Actor.TokenID,
		Action: "powerdns.zone.patch", TargetKind: "powerdns_zone", TargetID: upstream + ":" + powerDNSZoneID,
		Details: details, RequestDigest: digest[:],
	})
	if err != nil {
		return fmt.Errorf("%w: insert authorization denial audit: %v", ErrAuditUnavailable, err)
	}
	return nil
}
