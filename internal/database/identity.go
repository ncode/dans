package database

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ncode/dans/internal/identifier"
	"github.com/ncode/dans/internal/page"
)

var handlePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?$`)

type IdentityCreate struct {
	Kind        string
	Handle      string
	DisplayName *string
}

type NullableStringPatch struct {
	Set   bool
	Value *string
}

type IdentityPatch struct {
	DisplayName NullableStringPatch
	Enabled     *bool
	Operator    *bool
}

type IdentityListOptions struct {
	Limit        int
	After        *page.Key
	Kind         *string
	Enabled      *bool
	Operator     *bool
	Handle       *string
	HandlePrefix *string
}

type IdentityPage struct {
	Items []Identity
	Next  *page.Key
}

func (s *Store) CreateIdentity(ctx context.Context, actor Actor, input IdentityCreate) (Identity, error) {
	if err := requireOperator(actor); err != nil {
		return Identity{}, err
	}
	if !validIdentityKind(input.Kind) || !validHandle(input.Handle) {
		return Identity{}, ErrInvalid
	}
	id, err := identifier.NewUUID()
	if err != nil {
		return Identity{}, fmt.Errorf("generate identity id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Identity{}, fmt.Errorf("begin create identity: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	created, err := q.CreateIdentity(ctx, CreateIdentityParams{
		ID:          id,
		Kind:        input.Kind,
		Handle:      input.Handle,
		DisplayName: input.DisplayName,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("create identity: %w", mapStoreError(err))
	}
	if err := recordManagementAudit(ctx, q, actor, "identity.create", "identity", created.ID, map[string]any{
		"kind": created.Kind, "handle": created.Handle,
	}); err != nil {
		return Identity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Identity{}, fmt.Errorf("commit create identity: %w", err)
	}
	return created, nil
}

func (s *Store) GetIdentity(ctx context.Context, actor Actor, id string) (Identity, error) {
	if err := requireOperator(actor); err != nil {
		return Identity{}, err
	}
	if identifier.ValidateUUID(id) != nil {
		return Identity{}, ErrInvalid
	}
	identity, err := s.queries.GetIdentityByID(ctx, id)
	if err != nil {
		return Identity{}, fmt.Errorf("get identity: %w", mapStoreError(err))
	}
	return identity, nil
}

func (s *Store) ListIdentities(ctx context.Context, actor Actor, options IdentityListOptions) (IdentityPage, error) {
	if err := requireOperator(actor); err != nil {
		return IdentityPage{}, err
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return IdentityPage{}, ErrInvalid
	}
	if options.Kind != nil && !validIdentityKind(*options.Kind) || options.Handle != nil && !validHandle(*options.Handle) || options.HandlePrefix != nil && !validHandlePrefix(*options.HandlePrefix) {
		return IdentityPage{}, ErrInvalid
	}
	params := ListIdentitiesParams{
		Kind:         options.Kind,
		Enabled:      options.Enabled,
		IsOperator:   options.Operator,
		Handle:       options.Handle,
		HandlePrefix: options.HandlePrefix,
		RowLimit:     int32(limit + 1),
	}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return IdentityPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	items, err := s.queries.ListIdentities(ctx, params)
	if err != nil {
		return IdentityPage{}, fmt.Errorf("list identities: %w", err)
	}
	result := IdentityPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}

func (s *Store) PatchIdentity(ctx context.Context, actor Actor, id string, patch IdentityPatch) (Identity, error) {
	if err := requireOperator(actor); err != nil {
		return Identity{}, err
	}
	if identifier.ValidateUUID(id) != nil {
		return Identity{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Identity{}, fmt.Errorf("begin patch identity: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	if _, err := q.LockOperatorGuard(ctx); err != nil {
		return Identity{}, fmt.Errorf("lock operator guard: %w", err)
	}
	current, err := q.GetIdentityForUpdate(ctx, id)
	if err != nil {
		return Identity{}, fmt.Errorf("get identity for update: %w", mapStoreError(err))
	}
	if !patch.DisplayName.Set && patch.Enabled == nil && patch.Operator == nil {
		return current, nil
	}
	displayName := current.DisplayName
	if patch.DisplayName.Set {
		displayName = patch.DisplayName.Value
	}
	enabled := current.Enabled
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	operator := current.IsOperator
	if patch.Operator != nil {
		operator = *patch.Operator
	}
	if current.Enabled && current.IsOperator && (!enabled || !operator) {
		count, err := q.CountEnabledOperators(ctx)
		if err != nil {
			return Identity{}, fmt.Errorf("count enabled operators: %w", err)
		}
		if count <= 1 {
			return Identity{}, ErrConflict
		}
	}
	updated, err := q.UpdateIdentity(ctx, UpdateIdentityParams{
		ID:          id,
		DisplayName: displayName,
		Enabled:     enabled,
		IsOperator:  operator,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("update identity: %w", mapStoreError(err))
	}
	if err := recordManagementAudit(ctx, q, actor, "identity.update", "identity", id, map[string]any{
		"display_name_changed": patch.DisplayName.Set,
		"enabled":              updated.Enabled,
		"is_operator":          updated.IsOperator,
	}); err != nil {
		return Identity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Identity{}, fmt.Errorf("commit patch identity: %w", err)
	}
	return updated, nil
}

func recordManagementAudit(ctx context.Context, q *Queries, actor Actor, action, targetKind, targetID string, details any) error {
	id, err := identifier.NewUUID()
	if err != nil {
		return fmt.Errorf("generate audit id: %w", err)
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode audit details: %w", err)
	}
	_, err = q.InsertManagementAudit(ctx, InsertManagementAuditParams{
		ID:              id,
		RequestID:       actor.RequestID,
		ActorIdentityID: actor.IdentityID,
		ActorTokenID:    actor.TokenID,
		Action:          action,
		TargetKind:      targetKind,
		TargetID:        targetID,
		Result:          "succeeded",
		Details:         encoded,
	})
	if err != nil {
		return fmt.Errorf("%w: insert management audit: %v", ErrAuditUnavailable, err)
	}
	return nil
}

func rollback(tx pgx.Tx) {
	_ = tx.Rollback(context.Background())
}

func validIdentityKind(kind string) bool {
	return kind == "user" || kind == "service"
}

func validHandle(handle string) bool {
	return len(handle) <= 63 && handlePattern.MatchString(handle)
}

func validHandlePrefix(prefix string) bool {
	return len(prefix) > 0 && len(prefix) <= 63 && handlePrefixPattern.MatchString(prefix)
}

var handlePrefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
