package database

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ncode/dans/internal/dnsname"
	"github.com/ncode/dans/internal/identifier"
	"github.com/ncode/dans/internal/page"
)

type ZoneBindingInput struct {
	Upstream       string
	PowerDNSZoneID string
	ZoneName       string
}

type ZoneBindingListOptions struct {
	Limit    int
	After    *page.Key
	Status   *string
	ZoneName *string
}

type ZoneBindingPage struct {
	Items []ZoneBinding
	Next  *page.Key
}

func (s *Store) BindCreatedZone(ctx context.Context, actor Actor, input ZoneBindingInput) (ZoneBinding, error) {
	return s.ensureZoneBinding(ctx, actor, input, true)
}

func (s *Store) EnsureZoneBinding(ctx context.Context, actor Actor, input ZoneBindingInput) (ZoneBinding, error) {
	return s.ensureZoneBinding(ctx, actor, input, false)
}

func (s *Store) ensureZoneBinding(ctx context.Context, actor Actor, input ZoneBindingInput, allowNewLifetime bool) (ZoneBinding, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneBinding{}, err
	}
	zone, err := dnsname.Parse(input.ZoneName)
	if err != nil || !validHandle(input.Upstream) || input.PowerDNSZoneID == "" || len(input.PowerDNSZoneID) > 255 {
		return ZoneBinding{}, ErrInvalid
	}
	input.ZoneName = zone.String()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("begin bind zone: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	lockKeys := []string{"id:" + input.Upstream + ":" + input.PowerDNSZoneID, "name:" + input.Upstream + ":" + input.ZoneName}
	slices.Sort(lockKeys)
	for _, key := range lockKeys {
		if err := q.LockZoneBindingKey(ctx, key); err != nil {
			return ZoneBinding{}, fmt.Errorf("lock zone binding key: %w", err)
		}
	}
	candidates, err := q.ListActiveZoneBindingCandidates(ctx, ListActiveZoneBindingCandidatesParams{
		Upstream: input.Upstream, PowerDNSZoneID: input.PowerDNSZoneID, ZoneName: input.ZoneName,
	})
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("list active zone binding candidates: %w", err)
	}
	if len(candidates) > 0 {
		if len(candidates) == 1 && candidates[0].PowerDNSZoneID == input.PowerDNSZoneID && candidates[0].ZoneName == input.ZoneName {
			return candidates[0], nil
		}
		return ZoneBinding{}, ErrConflict
	}
	if !allowNewLifetime {
		history, err := q.ZoneBindingHistoryExists(ctx, ZoneBindingHistoryExistsParams{
			Upstream: input.Upstream, PowerDNSZoneID: input.PowerDNSZoneID, ZoneName: input.ZoneName,
		})
		if err != nil {
			return ZoneBinding{}, fmt.Errorf("check zone binding history: %w", err)
		}
		if history {
			return ZoneBinding{}, ErrConflict
		}
	}
	generation, err := q.NextZoneBindingGeneration(ctx, NextZoneBindingGenerationParams{Upstream: input.Upstream, ZoneName: input.ZoneName})
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("select next zone binding generation: %w", err)
	}
	id, err := identifier.NewUUID()
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("generate zone binding id: %w", err)
	}
	created, err := q.CreateZoneBinding(ctx, CreateZoneBindingParams{
		ID: id, Generation: generation, Upstream: input.Upstream,
		PowerDNSZoneID: input.PowerDNSZoneID, ZoneName: input.ZoneName,
		CreatedByIdentityID: actor.IdentityID,
	})
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("create zone binding: %w", mapStoreError(err))
	}
	if err := recordManagementAudit(ctx, q, actor, "zone_binding.create", "zone_binding", id, map[string]any{
		"upstream": input.Upstream, "powerdns_zone_id": input.PowerDNSZoneID,
		"zone_name": input.ZoneName, "generation": generation,
	}); err != nil {
		return ZoneBinding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ZoneBinding{}, fmt.Errorf("commit zone binding: %w", err)
	}
	return created, nil
}

func (s *Store) GetZoneBinding(ctx context.Context, actor Actor, id string) (ZoneBinding, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneBinding{}, err
	}
	if identifier.ValidateUUID(id) != nil {
		return ZoneBinding{}, ErrInvalid
	}
	binding, err := s.queries.GetZoneBindingByID(ctx, id)
	if err != nil {
		return ZoneBinding{}, fmt.Errorf("get zone binding: %w", mapStoreError(err))
	}
	return binding, nil
}

func (s *Store) ListZoneBindings(ctx context.Context, actor Actor, options ZoneBindingListOptions) (ZoneBindingPage, error) {
	if err := requireOperator(actor); err != nil {
		return ZoneBindingPage{}, err
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return ZoneBindingPage{}, ErrInvalid
	}
	params := ListZoneBindingsParams{RowLimit: int32(limit + 1)}
	if options.Status != nil {
		if *options.Status != "active" && *options.Status != "retired" {
			return ZoneBindingPage{}, ErrInvalid
		}
		params.Status = *options.Status
	}
	if options.ZoneName != nil {
		zone, err := dnsname.Parse(*options.ZoneName)
		if err != nil {
			return ZoneBindingPage{}, ErrInvalid
		}
		params.ZoneName = zone.String()
	}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return ZoneBindingPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	items, err := s.queries.ListZoneBindings(ctx, params)
	if err != nil {
		return ZoneBindingPage{}, fmt.Errorf("list zone bindings: %w", err)
	}
	result := ZoneBindingPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}
