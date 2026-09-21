package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ncode/dans/internal/identifier"
	"github.com/ncode/dans/internal/page"
)

func (s *Store) GetCurrentIdentity(ctx context.Context, actor Actor) (Identity, error) {
	if err := requireActor(actor); err != nil {
		return Identity{}, err
	}
	identity, err := s.queries.GetIdentityByID(ctx, actor.IdentityID)
	if err != nil {
		return Identity{}, fmt.Errorf("get current identity: %w", mapStoreError(err))
	}
	return identity, nil
}

func (s *Store) ListGroupMembers(ctx context.Context, actor Actor, groupID string, options IdentityListOptions) (IdentityPage, error) {
	if err := requireOperator(actor); err != nil {
		return IdentityPage{}, err
	}
	if identifier.ValidateUUID(groupID) != nil || options.Kind != nil || options.Enabled != nil || options.Operator != nil || options.Handle != nil || options.HandlePrefix != nil && !validHandlePrefix(*options.HandlePrefix) {
		return IdentityPage{}, ErrInvalid
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return IdentityPage{}, ErrInvalid
	}
	if _, err := s.queries.GetGroupByID(ctx, groupID); err != nil {
		return IdentityPage{}, fmt.Errorf("get member group: %w", mapStoreError(err))
	}
	params := ListGroupMembersParams{GroupID: groupID, HandlePrefix: options.HandlePrefix, RowLimit: int32(limit + 1)}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return IdentityPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	items, err := s.queries.ListGroupMembers(ctx, params)
	if err != nil {
		return IdentityPage{}, fmt.Errorf("list group members: %w", err)
	}
	result := IdentityPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}

func (s *Store) ListCurrentIdentityGroups(ctx context.Context, actor Actor, options GroupListOptions) (GroupPage, error) {
	if err := requireActor(actor); err != nil {
		return GroupPage{}, err
	}
	return s.listIdentityGroups(ctx, actor.IdentityID, options)
}

func (s *Store) ListIdentityGroups(ctx context.Context, actor Actor, identityID string, options GroupListOptions) (GroupPage, error) {
	if _, err := s.GetIdentity(ctx, actor, identityID); err != nil {
		return GroupPage{}, err
	}
	return s.listIdentityGroups(ctx, identityID, options)
}

func (s *Store) listIdentityGroups(ctx context.Context, identityID string, options GroupListOptions) (GroupPage, error) {
	if options.Enabled != nil || options.Handle != nil || options.HandlePrefix != nil && !validHandlePrefix(*options.HandlePrefix) {
		return GroupPage{}, ErrInvalid
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return GroupPage{}, ErrInvalid
	}
	params := ListIdentityGroupsParams{IdentityID: identityID, HandlePrefix: options.HandlePrefix, RowLimit: int32(limit + 1)}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return GroupPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	items, err := s.queries.ListIdentityGroups(ctx, params)
	if err != nil {
		return GroupPage{}, fmt.Errorf("list current identity groups: %w", err)
	}
	result := GroupPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}
