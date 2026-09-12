package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/dnsname"
	"github.com/ncode/dans/internal/identifier"
)

// BrowseClaim fences one collection against lease replacement and zone lifetime.
type BrowseClaim struct{ Zone, ZoneID, Lease string }

// BrowseChangeClaim identifies one durable affected-set read, never a DNS write.
type BrowseChangeClaim struct {
	BrowseClaim
	Operation, Name, Type string
}

func (s *Store) ClaimBrowseFull(ctx context.Context, upstream string) (BrowseClaim, error) {
	lease, err := identifier.NewUUID()
	if err != nil {
		return BrowseClaim{}, err
	}
	claim := BrowseClaim{Lease: lease}
	err = s.db.QueryRow(ctx, `
  WITH candidate AS (
   SELECT id FROM browse_zones WHERE upstream=$1
   AND (full_lease_until IS NULL OR full_lease_until<now()) AND retry_at<=now()
   AND (full_requested OR generation IS NULL OR full_lease IS NOT NULL OR
       (last_viewed_at>now()-interval '5 minutes' AND refreshed_at<now()-interval '5 minutes'))
   ORDER BY last_viewed_at DESC FOR UPDATE SKIP LOCKED LIMIT 1
  ) UPDATE browse_zones z SET full_lease=$2, full_lease_until=now()+interval '30 seconds',
    full_started_at=clock_timestamp(), full_requested=true,error=NULL
   FROM candidate c WHERE z.id=c.id RETURNING z.id::text,z.zone_id`, upstream, lease).Scan(&claim.Zone, &claim.ZoneID)
	if err != nil {
		return BrowseClaim{}, mapStoreError(err)
	}
	return claim, nil
}

// RenewBrowseFull holds only a metadata lock, independent of API collection.
func (s *Store) RenewBrowseFull(ctx context.Context, c BrowseClaim) error {
	tag, err := s.db.Exec(ctx, `UPDATE browse_zones SET full_lease_until=now()+interval '30 seconds'
  WHERE id=$1 AND full_lease=$2 AND full_lease_until>now()`, c.Zone, c.Lease)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrBrowseLeaseLost
	}
	return err
}

func canonicalBrowseSet(raw json.RawMessage) (api.RRSet, error) {
	if len(raw) > 8<<20 {
		return api.RRSet{}, ErrInvalid
	}
	var value struct {
		api.RRSet
		TTL *int `json:"ttl"`
	}
	if err := json.Unmarshal(raw, &value); err != nil || value.TTL == nil {
		return api.RRSet{}, ErrInvalid
	}
	set := value.RRSet
	set.Ttl = *value.TTL
	name, err := dnsname.Parse(set.Name)
	if err != nil || !validUniqueRecordTypes([]string{set.Type}) || set.Ttl < 0 || set.Records == nil {
		return set, ErrInvalid
	}
	set.Name = name.String()
	return set, nil
}

func (s *Store) StageBrowseRRsets(ctx context.Context, c BrowseClaim, sets []json.RawMessage) error {
	if len(sets) > 256 {
		return ErrInvalid
	}
	rows := make([][]any, len(sets))
	for i, raw := range sets {
		set, err := canonicalBrowseSet(raw)
		if err != nil {
			return err
		}
		// Preserve the complete API payload, including extension fields.
		rows[i] = []any{c.Zone, c.Lease, set.Name, set.Type, raw}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var valid int
	err = tx.QueryRow(ctx, `SELECT 1 FROM browse_zones WHERE id=$1 AND full_lease=$2
   AND full_lease_until>now() FOR SHARE`, c.Zone, c.Lease).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBrowseLeaseLost
	}
	if err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"browse_rrsets"}, []string{"zone", "generation", "name", "type", "payload"}, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("stage browse sets: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Store) PublishBrowseFull(ctx context.Context, c BrowseClaim) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var valid int
	err = tx.QueryRow(ctx, `SELECT 1 FROM browse_zones WHERE id=$1 AND full_lease=$2
   AND full_lease_until>now() FOR UPDATE`, c.Zone, c.Lease).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBrowseLeaseLost
	}
	if err != nil {
		return err
	}
	// Incremental results written during collection may be absent from the new
	// snapshot. Replay those *reads*, keeping every pending operation durable.
	if _, err := tx.Exec(ctx, `UPDATE browse_changes c SET processed=false
   FROM browse_zones z WHERE c.zone=z.id AND z.id=$1 AND c.changed_at>=z.full_started_at`, c.Zone); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM browse_changes WHERE zone=$1 AND processed`, c.Zone); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE browse_zones SET generation=$2,revision=revision+1,
  refreshed_at=now(),error=NULL,full_requested=false,full_lease=NULL,full_lease_until=NULL,
  full_started_at=NULL,retry_at=now() WHERE id=$1`, c.Zone, c.Lease); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) FailBrowseFull(ctx context.Context, c BrowseClaim) error {
	tag, err := s.db.Exec(ctx, `UPDATE browse_zones SET full_requested=true,full_lease=NULL,
 full_lease_until=NULL,full_started_at=NULL,retry_at=now()+interval '30 seconds',
 error='Refresh failed. Retry or wait for the next refresh.' WHERE id=$1 AND full_lease=$2`, c.Zone, c.Lease)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrBrowseLeaseLost
	}
	return err
}

func (s *Store) ClaimBrowseChange(ctx context.Context, upstream string) (BrowseChangeClaim, error) {
	lease, err := identifier.NewUUID()
	if err != nil {
		return BrowseChangeClaim{}, err
	}
	claim := BrowseChangeClaim{BrowseClaim: BrowseClaim{Lease: lease}}
	err = s.db.QueryRow(ctx, `WITH candidate AS (
  SELECT c.zone,c.operation,c.name,c.type FROM browse_changes c JOIN browse_zones z ON z.id=c.zone
  WHERE z.upstream=$1 AND NOT c.processed AND c.ready_at<=now()
  AND (c.lease_until IS NULL OR c.lease_until<now())
  AND NOT EXISTS (SELECT 1 FROM browse_changes busy WHERE busy.zone=c.zone
      AND busy.name=c.name AND busy.type=c.type AND busy.lease_until>now())
  ORDER BY c.ready_at FOR UPDATE OF z,c SKIP LOCKED LIMIT 1
 ) UPDATE browse_changes c SET lease=$2,lease_until=now()+interval '30 seconds'
 FROM candidate k,browse_zones z WHERE c.zone=k.zone AND c.operation=k.operation
 AND c.name=k.name AND c.type=k.type AND z.id=c.zone
 RETURNING c.zone::text,z.zone_id,c.operation::text,c.name,c.type`, upstream, lease).Scan(&claim.Zone, &claim.ZoneID, &claim.Operation, &claim.Name, &claim.Type)
	if err != nil {
		return BrowseChangeClaim{}, mapStoreError(err)
	}
	return claim, nil
}

func (s *Store) PublishBrowseChange(ctx context.Context, c BrowseChangeClaim, sets []json.RawMessage) error {
	if len(sets) > 1 {
		return ErrInvalid
	}
	if len(sets) == 1 {
		set, err := canonicalBrowseSet(sets[0])
		if err != nil || set.Name != c.Name || set.Type != c.Type {
			return ErrInvalid
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var generation, fullLease *string
	// Lock metadata before the work row, matching full publication and lifecycle.
	err = tx.QueryRow(ctx, `SELECT generation::text,full_lease::text FROM browse_zones WHERE id=$1 FOR UPDATE`, c.Zone).Scan(&generation, &fullLease)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBrowseLeaseLost
	}
	if err != nil {
		return err
	}
	var valid int
	err = tx.QueryRow(ctx, `SELECT 1 FROM browse_changes WHERE zone=$1 AND operation=$2 AND name=$3 AND type=$4
   AND lease=$5 AND lease_until>now() FOR UPDATE`, c.Zone, c.Operation, c.Name, c.Type, c.Lease).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBrowseLeaseLost
	}
	if err != nil {
		return err
	}
	if c.Name == "" || generation == nil {
		if _, err := tx.Exec(ctx, `UPDATE browse_zones SET full_requested=true,retry_at=now() WHERE id=$1`, c.Zone); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `DELETE FROM browse_rrsets WHERE zone=$1 AND generation=$2 AND name=$3 AND type=$4`, c.Zone, *generation, c.Name, c.Type); err != nil {
			return err
		}
		if len(sets) == 1 {
			if _, err := tx.Exec(ctx, `INSERT INTO browse_rrsets(zone,generation,name,type,payload)VALUES($1,$2,$3,$4,$5)`, c.Zone, *generation, c.Name, c.Type, sets[0]); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE browse_zones SET revision=revision+1 WHERE id=$1`, c.Zone); err != nil {
			return err
		}
	}
	if fullLease != nil {
		_, err = tx.Exec(ctx, `UPDATE browse_changes SET processed=true,changed_at=clock_timestamp(),lease=NULL,lease_until=NULL WHERE zone=$1 AND operation=$2 AND name=$3 AND type=$4`, c.Zone, c.Operation, c.Name, c.Type)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM browse_changes WHERE zone=$1 AND operation=$2 AND name=$3 AND type=$4`, c.Zone, c.Operation, c.Name, c.Type)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FailBrowseChange retains read work and avoids retrying DNS mutations.
func (s *Store) FailBrowseChange(ctx context.Context, c BrowseChangeClaim) error {
	_, err := s.db.Exec(ctx, `UPDATE browse_changes SET lease=NULL,lease_until=NULL,ready_at=now()+interval '5 seconds'
 WHERE zone=$1 AND operation=$2 AND name=$3 AND type=$4 AND lease=$5`, c.Zone, c.Operation, c.Name, c.Type, c.Lease)
	return err
}

// CleanupBrowseRows deletes a bounded batch of unpublished or retired snapshots.
func (s *Store) CleanupBrowseRows(ctx context.Context, zone string) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM browse_rrsets WHERE ctid IN (
 SELECT r.ctid FROM browse_rrsets r JOIN browse_zones z ON r.zone=z.id
 WHERE z.id=$1 AND r.generation IS DISTINCT FROM z.generation AND r.generation IS DISTINCT FROM z.full_lease LIMIT 2048)`, zone)
	return tag.RowsAffected(), err
}
