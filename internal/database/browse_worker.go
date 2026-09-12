package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ncode/dans/internal/upstream"
	"golang.org/x/sync/errgroup"
)

// BrowseWorker runs one full collection and one affected-set reader per instance.
// Shared PostgreSQL leases coalesce requests and recover interrupted readers.
type BrowseWorker struct {
	store      *Store
	client     *upstream.Client
	upstreamID string
	ready      func() bool
}

func NewBrowseWorker(store *Store, client *upstream.Client, upstreamID string, ready func() bool) *BrowseWorker {
	return &BrowseWorker{store: store, client: client, upstreamID: upstreamID, ready: ready}
}

// Run owns every worker goroutine; all exit and are joined on cancellation.
func (w *BrowseWorker) Run(ctx context.Context) error {
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return w.fullLoop(ctx) })
	group.Go(func() error { return w.changeLoop(ctx) })
	err := group.Wait()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func browsePause(ctx context.Context) error {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (w *BrowseWorker) fullLoop(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !w.ready() {
			if err := browsePause(ctx); err != nil {
				return err
			}
			continue
		}
		claim, err := w.store.ClaimBrowseFull(ctx, w.upstreamID)
		if errors.Is(err, ErrNotFound) {
			if err := browsePause(ctx); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("claim browse refresh: %w", err)
		}
		err = w.collect(ctx, claim)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			// A failed refresh publishes only sanitized freshness state; it never
			// replaces a complete generation, and retries only authoritative reads.
			if errors.Is(err, upstream.ErrBrowseZoneAbsent) {
				if _, err := w.store.db.Exec(ctx, `DELETE FROM browse_zones WHERE id=$1 AND full_lease=$2`, claim.Zone, claim.Lease); err != nil {
					return err
				}
			} else if err := w.store.FailBrowseFull(ctx, claim); err != nil && !errors.Is(err, ErrBrowseLeaseLost) {
				return err
			}
		}
		for {
			removed, err := w.store.CleanupBrowseRows(ctx, claim.Zone)
			if err != nil {
				return fmt.Errorf("clean browse generations: %w", err)
			}
			if removed == 0 {
				break
			}
		}
	}
}

func (w *BrowseWorker) collect(ctx context.Context, claim BrowseClaim) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)
	complete := make(chan struct{})
	group.Go(func() error {
		defer close(complete)
		sets := make([]json.RawMessage, 0, 256)
		bytes := 0
		flush := func() error {
			if len(sets) == 0 {
				return nil
			}
			if err := w.store.StageBrowseRRsets(ctx, claim, sets); err != nil {
				return err
			}
			clear(sets)
			sets = sets[:0]
			bytes = 0
			return nil
		}
		err := w.client.StreamZoneRRsets(ctx, claim.ZoneID, "", "", func(raw json.RawMessage) error {
			sets = append(sets, raw)
			bytes += len(raw)
			if len(sets) == 256 || bytes >= 1<<20 {
				return flush()
			}
			return nil
		})
		if err != nil {
			return err
		}
		if err := flush(); err != nil {
			return err
		}
		return w.store.PublishBrowseFull(ctx, claim)
	})
	group.Go(func() error {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-complete:
				return nil
			case <-ticker.C:
				if err := w.store.RenewBrowseFull(ctx, claim); err != nil {
					select {
					case <-complete:
						return nil
					default:
						return err
					}
				}
			}
		}
	})
	return group.Wait()
}

func (w *BrowseWorker) changeLoop(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !w.ready() {
			if err := browsePause(ctx); err != nil {
				return err
			}
			continue
		}
		claim, err := w.store.ClaimBrowseChange(ctx, w.upstreamID)
		if errors.Is(err, ErrNotFound) {
			if err := browsePause(ctx); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("claim browse change: %w", err)
		}
		var sets []json.RawMessage
		if claim.Name != "" {
			readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = w.client.StreamZoneRRsets(readCtx, claim.ZoneID, claim.Name, claim.Type, func(raw json.RawMessage) error {
				if len(sets) != 0 {
					return errors.New("upstream returned multiple exact RRsets")
				}
				sets = append(sets, raw)
				return nil
			})
			cancel()
		}
		if err == nil {
			err = w.store.PublishBrowseChange(ctx, claim, sets)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, upstream.ErrBrowseZoneAbsent) {
			if _, err := w.store.db.Exec(ctx, `DELETE FROM browse_zones WHERE id=$1`, claim.Zone); err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, ErrBrowseLeaseLost) {
			if err := w.store.FailBrowseChange(ctx, claim); err != nil {
				return err
			}
		}
	}
}
