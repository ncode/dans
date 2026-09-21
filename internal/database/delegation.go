package database

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	delegationdomain "github.com/ncode/dans/internal/delegation"
	"github.com/ncode/dans/internal/dnsname"
	"github.com/ncode/dans/internal/identifier"
	"github.com/ncode/dans/internal/page"
)

const maxDelegationSelectors = 100

type DelegationSelectorInput struct {
	Kind  delegationdomain.SelectorKind
	Value string
}

type DelegationCreate struct {
	ZoneBindingID     string
	GranteeIdentityID *string
	GranteeGroupID    *string
	Selectors         []DelegationSelectorInput
	RecordTypes       *[]string
	ChangeKinds       *[]string
}

type DelegationDetails struct {
	ZoneID        string
	ZoneName      string
	ID            string
	ZoneBindingID string
	GranteeKind   string
	GranteeID     string
	Selectors     []DelegationSelectorInput
	RecordTypes   []string
	ChangeKinds   []string
	CreatedAt     pgtype.Timestamptz
	RevokedAt     pgtype.Timestamptz
}

type DelegationListOptions struct {
	Limit         int
	After         *page.Key
	ZoneBindingID *string
	GranteeID     *string
	Active        *bool
}

type DelegationPage struct {
	Items []DelegationDetails
	Next  *page.Key
}

type compiledDelegationInput struct {
	selectors   []delegationdomain.Selector
	recordTypes []string
	changeKinds []string
}

func (s *Store) CreateDelegation(ctx context.Context, actor Actor, input DelegationCreate) (DelegationDetails, error) {
	if err := requireOperator(actor); err != nil {
		return DelegationDetails{}, err
	}
	if err := validateDelegationShape(input); err != nil {
		return DelegationDetails{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return DelegationDetails{}, fmt.Errorf("begin create delegation: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	binding, err := q.GetZoneBindingForUpdate(ctx, input.ZoneBindingID)
	if err != nil {
		return DelegationDetails{}, fmt.Errorf("get delegation zone binding: %w", mapStoreError(err))
	}
	if binding.RetiredAt.Valid {
		return DelegationDetails{}, ErrConflict
	}
	zone, err := dnsname.Parse(binding.ZoneName)
	if err != nil {
		return DelegationDetails{}, fmt.Errorf("stored zone binding name is invalid: %w", err)
	}
	compiled, err := compileDelegationInput(input, zone)
	if err != nil {
		return DelegationDetails{}, err
	}
	granteeKind, granteeID, err := validateDelegationGrantee(ctx, q, input)
	if err != nil {
		return DelegationDetails{}, err
	}
	id, err := identifier.NewUUID()
	if err != nil {
		return DelegationDetails{}, fmt.Errorf("generate delegation id: %w", err)
	}
	identityID, groupID := "", ""
	if granteeKind == "identity" {
		identityID = granteeID
	} else {
		groupID = granteeID
	}
	created, err := q.CreateDelegation(ctx, CreateDelegationParams{
		ID: id, ZoneBindingID: input.ZoneBindingID,
		GranteeIdentityID: identityID, GranteeGroupID: groupID,
		CreatedByIdentityID: actor.IdentityID,
	})
	if err != nil {
		return DelegationDetails{}, fmt.Errorf("create delegation: %w", mapStoreError(err))
	}
	selectors := make([]DelegationSelectorInput, len(compiled.selectors))
	for i, selector := range compiled.selectors {
		if err := q.CreateDelegationSelector(ctx, CreateDelegationSelectorParams{
			DelegationID:   id,
			Position:       int16(i + 1),
			Kind:           string(selector.Kind()),
			Selector:       selector.Pattern(),
			SQLLikePattern: selector.SQLLike(),
		}); err != nil {
			return DelegationDetails{}, fmt.Errorf("create delegation selector: %w", mapStoreError(err))
		}
		selectors[i] = DelegationSelectorInput{Kind: selector.Kind(), Value: selector.Pattern()}
	}
	for _, recordType := range compiled.recordTypes {
		if err := q.CreateDelegationRecordType(ctx, CreateDelegationRecordTypeParams{DelegationID: id, RecordType: recordType}); err != nil {
			return DelegationDetails{}, fmt.Errorf("create delegation record type: %w", mapStoreError(err))
		}
	}
	for _, changeKind := range compiled.changeKinds {
		if err := q.CreateDelegationChangeKind(ctx, CreateDelegationChangeKindParams{DelegationID: id, ChangeKind: changeKind}); err != nil {
			return DelegationDetails{}, fmt.Errorf("create delegation change kind: %w", mapStoreError(err))
		}
	}
	if err := recordManagementAudit(ctx, q, actor, "delegation.create", "delegation", id, map[string]any{
		"zone_binding_id": input.ZoneBindingID, "grantee_kind": granteeKind, "grantee_id": granteeID,
	}); err != nil {
		return DelegationDetails{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DelegationDetails{}, fmt.Errorf("commit create delegation: %w", err)
	}
	return DelegationDetails{
		ID: id, ZoneBindingID: created.ZoneBindingID, GranteeKind: granteeKind, GranteeID: granteeID,
		Selectors: selectors, RecordTypes: compiled.recordTypes, ChangeKinds: compiled.changeKinds,
		CreatedAt: created.CreatedAt, RevokedAt: created.RevokedAt,
	}, nil
}

func (s *Store) GetDelegation(ctx context.Context, actor Actor, id string) (DelegationDetails, error) {
	if err := requireOperator(actor); err != nil {
		return DelegationDetails{}, err
	}
	if identifier.ValidateUUID(id) != nil {
		return DelegationDetails{}, ErrInvalid
	}
	row, err := s.queries.GetDelegationDetails(ctx, id)
	if err != nil {
		return DelegationDetails{}, fmt.Errorf("get delegation: %w", mapStoreError(err))
	}
	details := DelegationDetails{
		ID: row.ID, ZoneBindingID: row.ZoneBindingID, GranteeKind: row.GranteeKind,
		GranteeID: row.GranteeID, CreatedAt: row.CreatedAt, RevokedAt: row.RevokedAt,
	}
	items := []DelegationDetails{details}
	if err := loadDelegationChildren(ctx, s.queries, items); err != nil {
		return DelegationDetails{}, err
	}
	return items[0], nil
}

func (s *Store) ListDelegations(ctx context.Context, actor Actor, options DelegationListOptions) (DelegationPage, error) {
	if err := requireOperator(actor); err != nil {
		return DelegationPage{}, err
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return DelegationPage{}, ErrInvalid
	}
	params := ListDelegationDetailsParams{RowLimit: int32(limit + 1)}
	if options.ZoneBindingID != nil {
		if identifier.ValidateUUID(*options.ZoneBindingID) != nil {
			return DelegationPage{}, ErrInvalid
		}
		params.ZoneBindingID = *options.ZoneBindingID
	}
	if options.GranteeID != nil {
		if identifier.ValidateUUID(*options.GranteeID) != nil {
			return DelegationPage{}, ErrInvalid
		}
		params.GranteeID = *options.GranteeID
	}
	params.Active = options.Active
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return DelegationPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	rows, err := s.queries.ListDelegationDetails(ctx, params)
	if err != nil {
		return DelegationPage{}, fmt.Errorf("list delegations: %w", err)
	}
	items := make([]DelegationDetails, len(rows))
	for i, row := range rows {
		items[i] = DelegationDetails{
			ID: row.ID, ZoneBindingID: row.ZoneBindingID, GranteeKind: row.GranteeKind,
			GranteeID: row.GranteeID, CreatedAt: row.CreatedAt, RevokedAt: row.RevokedAt,
		}
	}
	if err := loadDelegationChildren(ctx, s.queries, items); err != nil {
		return DelegationPage{}, err
	}
	result := DelegationPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}

func (s *Store) ListCurrentIdentityDelegations(ctx context.Context, actor Actor, options DelegationListOptions) (DelegationPage, error) {
	if err := requireActor(actor); err != nil {
		return DelegationPage{}, err
	}
	return s.listIdentityDelegations(ctx, actor.IdentityID, options)
}

func (s *Store) ListIdentityDelegations(ctx context.Context, actor Actor, identityID string, options DelegationListOptions) (DelegationPage, error) {
	if _, err := s.GetIdentity(ctx, actor, identityID); err != nil {
		return DelegationPage{}, err
	}
	return s.listIdentityDelegations(ctx, identityID, options)
}

func (s *Store) listIdentityDelegations(ctx context.Context, identityID string, options DelegationListOptions) (DelegationPage, error) {
	if options.ZoneBindingID != nil || options.GranteeID != nil || options.Active != nil {
		return DelegationPage{}, ErrInvalid
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return DelegationPage{}, ErrInvalid
	}
	params := ListEffectiveDelegationDetailsParams{IdentityID: identityID, RowLimit: int32(limit + 1)}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return DelegationPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	rows, err := s.queries.ListEffectiveDelegationDetails(ctx, params)
	if err != nil {
		return DelegationPage{}, fmt.Errorf("list current identity delegations: %w", err)
	}
	items := make([]DelegationDetails, len(rows))
	for i, row := range rows {
		items[i] = DelegationDetails{
			ID: row.ID, ZoneBindingID: row.ZoneBindingID, GranteeKind: row.GranteeKind, ZoneID: row.PowerDNSZoneID, ZoneName: row.ZoneName,
			GranteeID: row.GranteeID, CreatedAt: row.CreatedAt, RevokedAt: row.RevokedAt,
		}
	}
	if err := loadDelegationChildren(ctx, s.queries, items); err != nil {
		return DelegationPage{}, err
	}
	result := DelegationPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}

func (s *Store) RevokeDelegation(ctx context.Context, actor Actor, id string) error {
	if err := requireOperator(actor); err != nil {
		return err
	}
	if identifier.ValidateUUID(id) != nil {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke delegation: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	current, err := q.GetDelegationForUpdate(ctx, id)
	if err != nil {
		return fmt.Errorf("get delegation for revoke: %w", mapStoreError(err))
	}
	if current.RevokedAt.Valid {
		return nil
	}
	if _, err := q.RevokeDelegation(ctx, RevokeDelegationParams{ID: id, RevokedByIdentityID: actor.IdentityID}); err != nil {
		return fmt.Errorf("revoke delegation: %w", mapStoreError(err))
	}
	if err := recordManagementAudit(ctx, q, actor, "delegation.revoke", "delegation", id, map[string]any{
		"zone_binding_id": current.ZoneBindingID,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke delegation: %w", err)
	}
	return nil
}

func loadDelegationChildren(ctx context.Context, q *Queries, items []DelegationDetails) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	indexes := make(map[string]int, len(items))
	for i := range items {
		ids[i] = items[i].ID
		indexes[items[i].ID] = i
	}
	selectors, err := q.ListDelegationSelectorsByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list delegation selectors: %w", err)
	}
	for _, selector := range selectors {
		i := indexes[selector.DelegationID]
		items[i].Selectors = append(items[i].Selectors, DelegationSelectorInput{
			Kind: delegationdomain.SelectorKind(selector.Kind), Value: selector.Selector,
		})
	}
	recordTypes, err := q.ListDelegationRecordTypesByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list delegation record types: %w", err)
	}
	for _, recordType := range recordTypes {
		i := indexes[recordType.DelegationID]
		items[i].RecordTypes = append(items[i].RecordTypes, recordType.RecordType)
	}
	changeKinds, err := q.ListDelegationChangeKindsByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list delegation change kinds: %w", err)
	}
	for _, changeKind := range changeKinds {
		i := indexes[changeKind.DelegationID]
		items[i].ChangeKinds = append(items[i].ChangeKinds, changeKind.ChangeKind)
	}
	return nil
}

func validateDelegationShape(input DelegationCreate) error {
	if identifier.ValidateUUID(input.ZoneBindingID) != nil || (input.GranteeIdentityID == nil) == (input.GranteeGroupID == nil) {
		return ErrInvalid
	}
	if input.GranteeIdentityID != nil && identifier.ValidateUUID(*input.GranteeIdentityID) != nil || input.GranteeGroupID != nil && identifier.ValidateUUID(*input.GranteeGroupID) != nil {
		return ErrInvalid
	}
	if len(input.Selectors) == 0 || len(input.Selectors) > maxDelegationSelectors {
		return ErrInvalid
	}
	if input.RecordTypes != nil && len(*input.RecordTypes) == 0 || input.ChangeKinds != nil && len(*input.ChangeKinds) == 0 {
		return ErrInvalid
	}
	return nil
}

func compileDelegationInput(input DelegationCreate, zone dnsname.Name) (compiledDelegationInput, error) {
	compiled := compiledDelegationInput{}
	seenSelectors := make(map[string]struct{}, len(input.Selectors))
	for _, value := range input.Selectors {
		selector, err := delegationdomain.CompileSelector(value.Kind, value.Value, zone)
		if err != nil {
			return compiledDelegationInput{}, ErrInvalid
		}
		key := string(selector.Kind()) + "\x00" + selector.Pattern()
		if _, exists := seenSelectors[key]; exists {
			return compiledDelegationInput{}, ErrInvalid
		}
		seenSelectors[key] = struct{}{}
		compiled.selectors = append(compiled.selectors, selector)
	}
	if input.RecordTypes != nil {
		compiled.recordTypes = slices.Clone(*input.RecordTypes)
		if !validUniqueRecordTypes(compiled.recordTypes) {
			return compiledDelegationInput{}, ErrInvalid
		}
		slices.Sort(compiled.recordTypes)
	}
	if input.ChangeKinds != nil {
		compiled.changeKinds = slices.Clone(*input.ChangeKinds)
		if !validUniqueChangeKinds(compiled.changeKinds) {
			return compiledDelegationInput{}, ErrInvalid
		}
		slices.Sort(compiled.changeKinds)
	}
	return compiled, nil
}

func validateDelegationGrantee(ctx context.Context, q *Queries, input DelegationCreate) (string, string, error) {
	if input.GranteeIdentityID != nil {
		if _, err := q.GetIdentityByID(ctx, *input.GranteeIdentityID); err != nil {
			return "", "", fmt.Errorf("get delegation identity grantee: %w", mapStoreError(err))
		}
		return "identity", *input.GranteeIdentityID, nil
	}
	if _, err := q.GetGroupByID(ctx, *input.GranteeGroupID); err != nil {
		return "", "", fmt.Errorf("get delegation group grantee: %w", mapStoreError(err))
	}
	return "group", *input.GranteeGroupID, nil
}

func validUniqueRecordTypes(values []string) bool {
	if len(values) > maxDelegationSelectors {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if len(value) == 0 || len(value) > 32 || value[0] < 'A' || value[0] > 'Z' {
			return false
		}
		for _, c := range value[1:] {
			if !('A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-') {
				return false
			}
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validUniqueChangeKinds(values []string) bool {
	if len(values) > 4 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		switch value {
		case "REPLACE", "DELETE", "EXTEND", "PRUNE":
		default:
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

type RetainedAssignment struct {
	DelegationDetails
	IdentityEnabled bool
	GroupEnabled    *bool
	GroupHandle     *string
	BindingStatus   string
	Effective       bool
}
type RetainedAssignmentPage struct {
	Items []RetainedAssignment
	Next  *page.Key
}

func (s *Store) ListIdentityAssignments(ctx context.Context, actor Actor, identityID string, options DelegationListOptions) (RetainedAssignmentPage, error) {
	if _, err := s.GetIdentity(ctx, actor, identityID); err != nil {
		return RetainedAssignmentPage{}, err
	}
	if options.ZoneBindingID != nil || options.GranteeID != nil || options.Active != nil {
		return RetainedAssignmentPage{}, ErrInvalid
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return RetainedAssignmentPage{}, ErrInvalid
	}
	params := ListIdentityAssignmentDetailsParams{IdentityID: identityID, RowLimit: int32(limit + 1)}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return RetainedAssignmentPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	rows, err := s.queries.ListIdentityAssignmentDetails(ctx, params)
	if err != nil {
		return RetainedAssignmentPage{}, fmt.Errorf("list identity assignments: %w", err)
	}
	details := make([]DelegationDetails, len(rows))
	for i, row := range rows {
		details[i] = DelegationDetails{ID: row.ID, ZoneBindingID: row.ZoneBindingID, ZoneID: row.PowerDNSZoneID, ZoneName: row.ZoneName, GranteeKind: row.GranteeKind, GranteeID: row.GranteeID, CreatedAt: row.CreatedAt, RevokedAt: row.RevokedAt}
	}
	if err := loadDelegationChildren(ctx, s.queries, details); err != nil {
		return RetainedAssignmentPage{}, err
	}
	result := RetainedAssignmentPage{Items: make([]RetainedAssignment, len(rows))}
	for i, row := range rows {
		result.Items[i] = RetainedAssignment{DelegationDetails: details[i], IdentityEnabled: row.IdentityEnabled, GroupEnabled: row.GroupEnabled, GroupHandle: row.GroupHandle, BindingStatus: row.BindingStatus, Effective: row.Effective}
	}
	if len(rows) > limit {
		last := rows[limit-1]
		result.Items = result.Items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}
