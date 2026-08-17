package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ncode/dans/internal/identifier"
	"github.com/ncode/dans/internal/page"
)

type GroupCreate struct {
	Handle      string
	DisplayName *string
}

type GroupPatch struct {
	DisplayName NullableStringPatch
	Enabled     *bool
}

type GroupListOptions struct {
	Limit   int
	After   *page.Key
	Enabled *bool
	Handle  *string
}

type GroupPage struct {
	Items []Group
	Next  *page.Key
}

func (s *Store) CreateGroup(ctx context.Context, actor Actor, input GroupCreate) (Group, error) {
	if err := requireOperator(actor); err != nil {
		return Group{}, err
	}
	if !validHandle(input.Handle) {
		return Group{}, ErrInvalid
	}
	id, err := identifier.NewUUID()
	if err != nil {
		return Group{}, fmt.Errorf("generate group id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("begin create group: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	created, err := q.CreateGroup(ctx, CreateGroupParams{ID: id, Handle: input.Handle, DisplayName: input.DisplayName})
	if err != nil {
		return Group{}, fmt.Errorf("create group: %w", mapStoreError(err))
	}
	if err := recordManagementAudit(ctx, q, actor, "group.create", "group", id, map[string]any{"handle": created.Handle}); err != nil {
		return Group{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Group{}, fmt.Errorf("commit create group: %w", err)
	}
	return created, nil
}

func (s *Store) GetGroup(ctx context.Context, actor Actor, id string) (Group, error) {
	if err := requireOperator(actor); err != nil {
		return Group{}, err
	}
	if identifier.ValidateUUID(id) != nil {
		return Group{}, ErrInvalid
	}
	group, err := s.queries.GetGroupByID(ctx, id)
	if err != nil {
		return Group{}, fmt.Errorf("get group: %w", mapStoreError(err))
	}
	return group, nil
}

func (s *Store) ListGroups(ctx context.Context, actor Actor, options GroupListOptions) (GroupPage, error) {
	if err := requireOperator(actor); err != nil {
		return GroupPage{}, err
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return GroupPage{}, ErrInvalid
	}
	if options.Handle != nil && !validHandle(*options.Handle) {
		return GroupPage{}, ErrInvalid
	}
	params := ListGroupsParams{Enabled: options.Enabled, Handle: options.Handle, RowLimit: int32(limit + 1)}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return GroupPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	items, err := s.queries.ListGroups(ctx, params)
	if err != nil {
		return GroupPage{}, fmt.Errorf("list groups: %w", err)
	}
	result := GroupPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}

func (s *Store) PatchGroup(ctx context.Context, actor Actor, id string, patch GroupPatch) (Group, error) {
	if err := requireOperator(actor); err != nil {
		return Group{}, err
	}
	if identifier.ValidateUUID(id) != nil {
		return Group{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("begin patch group: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	current, err := q.GetGroupForUpdate(ctx, id)
	if err != nil {
		return Group{}, fmt.Errorf("get group for update: %w", mapStoreError(err))
	}
	if !patch.DisplayName.Set && patch.Enabled == nil {
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
	updated, err := q.UpdateGroup(ctx, UpdateGroupParams{ID: id, DisplayName: displayName, Enabled: enabled})
	if err != nil {
		return Group{}, fmt.Errorf("update group: %w", mapStoreError(err))
	}
	if err := recordManagementAudit(ctx, q, actor, "group.update", "group", id, map[string]any{
		"display_name_changed": patch.DisplayName.Set,
		"enabled":              updated.Enabled,
	}); err != nil {
		return Group{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Group{}, fmt.Errorf("commit patch group: %w", err)
	}
	return updated, nil
}

func (s *Store) AddGroupMember(ctx context.Context, actor Actor, groupID, identityID string) (GroupMembership, error) {
	if err := requireOperator(actor); err != nil {
		return GroupMembership{}, err
	}
	if identifier.ValidateUUID(groupID) != nil || identifier.ValidateUUID(identityID) != nil {
		return GroupMembership{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return GroupMembership{}, fmt.Errorf("begin add group member: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	if _, err := q.GetGroupByID(ctx, groupID); err != nil {
		return GroupMembership{}, fmt.Errorf("get membership group: %w", mapStoreError(err))
	}
	if _, err := q.GetIdentityByID(ctx, identityID); err != nil {
		if _, groupErr := q.GetGroupByID(ctx, identityID); groupErr == nil {
			return GroupMembership{}, ErrInvalid
		}
		return GroupMembership{}, fmt.Errorf("get membership identity: %w", mapStoreError(err))
	}
	inserted, err := q.AddGroupMembership(ctx, AddGroupMembershipParams{
		GroupID: groupID, IdentityID: identityID, AddedByIdentityID: actor.IdentityID,
	})
	if err != nil {
		return GroupMembership{}, fmt.Errorf("add group membership: %w", mapStoreError(err))
	}
	membership, err := q.GetGroupMembership(ctx, GetGroupMembershipParams{GroupID: groupID, IdentityID: identityID})
	if err != nil {
		return GroupMembership{}, fmt.Errorf("get group membership: %w", mapStoreError(err))
	}
	if inserted > 0 {
		targetID := groupID + ":" + identityID
		if err := recordManagementAudit(ctx, q, actor, "group.member.add", "group_membership", targetID, map[string]any{
			"group_id": groupID, "identity_id": identityID,
		}); err != nil {
			return GroupMembership{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return GroupMembership{}, fmt.Errorf("commit add group member: %w", err)
	}
	return membership, nil
}

func (s *Store) RemoveGroupMember(ctx context.Context, actor Actor, groupID, identityID string) error {
	if err := requireOperator(actor); err != nil {
		return err
	}
	if identifier.ValidateUUID(groupID) != nil || identifier.ValidateUUID(identityID) != nil {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin remove group member: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	if _, err := q.GetGroupByID(ctx, groupID); err != nil {
		return fmt.Errorf("get membership group: %w", mapStoreError(err))
	}
	if _, err := q.GetIdentityByID(ctx, identityID); err != nil {
		return fmt.Errorf("get membership identity: %w", mapStoreError(err))
	}
	removed, err := q.RemoveGroupMembership(ctx, RemoveGroupMembershipParams{GroupID: groupID, IdentityID: identityID})
	if err != nil {
		return fmt.Errorf("remove group membership: %w", err)
	}
	if removed > 0 {
		targetID := groupID + ":" + identityID
		if err := recordManagementAudit(ctx, q, actor, "group.member.remove", "group_membership", targetID, map[string]any{
			"group_id": groupID, "identity_id": identityID,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit remove group member: %w", err)
	}
	return nil
}
