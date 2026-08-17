package database

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// ErrMigrationDrift means an applied migration no longer matches the binary.
var ErrMigrationDrift = errors.New("database: migration drift")

var migrationName = regexp.MustCompile(`^(\d{6})_([a-z][a-z0-9_]*)\.sql$`)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

const migrationLockKey = 1145130579

type migration struct {
	version  int64
	name     string
	sql      string
	checksum [sha256.Size]byte
}

func (m migration) checksumHex() string {
	return hex.EncodeToString(m.checksum[:])
}

type appliedMigration struct {
	version  int64
	name     string
	checksum string
}

// SchemaStatus describes how the durable migration ledger relates to this binary.
type SchemaStatus struct {
	CurrentVersion int
	LatestVersion  int
	Compatible     bool
}

// Migrate applies every embedded migration in order under a database-wide lock.
func Migrate(ctx context.Context, conn *pgx.Conn) error {
	migrations, err := loadMigrations(embeddedMigrations)
	if err != nil {
		return err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.Background()) // best-effort after commit or failure
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", migrationLockKey, 1); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version bigint PRIMARY KEY CHECK (version > 0),
			name text NOT NULL CHECK (name <> ''),
			checksum text NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	applied, err := readAppliedMigrations(ctx, tx)
	if err != nil {
		return err
	}
	if err := validateLedger(migrations, applied); err != nil {
		return err
	}

	for _, m := range migrations[len(applied):] {
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			return fmt.Errorf("apply migration %06d_%s: %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO schema_migrations (version, name, checksum)
			VALUES ($1, $2, $3)`, m.version, m.name, m.checksumHex()); err != nil {
			return fmt.Errorf("record migration %06d_%s: %w", m.version, m.name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

// MigrationStatus inspects the migration ledger without creating or changing it.
func MigrationStatus(ctx context.Context, conn *pgx.Conn) (SchemaStatus, error) {
	migrations, err := loadMigrations(embeddedMigrations)
	if err != nil {
		return SchemaStatus{}, err
	}
	status := SchemaStatus{LatestVersion: int(migrations[len(migrations)-1].version), Compatible: true}

	var ledger *string
	if err := conn.QueryRow(ctx, `SELECT to_regclass('schema_migrations')::text`).Scan(&ledger); err != nil {
		return SchemaStatus{}, fmt.Errorf("locate migration ledger: %w", err)
	}
	if ledger == nil {
		return status, nil
	}
	applied, err := readAppliedMigrations(ctx, conn)
	if err != nil {
		return SchemaStatus{}, err
	}
	if len(applied) > 0 {
		status.CurrentVersion = int(applied[len(applied)-1].version)
	}
	status.Compatible = validateLedger(migrations, applied) == nil
	return status, nil
}

type migrationQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readAppliedMigrations(ctx context.Context, querier migrationQuerier) ([]appliedMigration, error) {
	rows, err := querier.Query(ctx, `
		SELECT version, name, checksum
		FROM schema_migrations
		ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()

	var applied []appliedMigration
	for rows.Next() {
		var m appliedMigration
		if err := rows.Scan(&m.version, &m.name, &m.checksum); err != nil {
			return nil, fmt.Errorf("scan migration ledger: %w", err)
		}
		applied = append(applied, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration ledger: %w", err)
	}
	return applied, nil
}

func loadMigrations(filesystem fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(filesystem, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("migration filename %q must match NNNNNN_name.sql", entry.Name())
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse migration version %q: %w", match[1], err)
		}
		data, err := fs.ReadFile(filesystem, path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{
			version:  version,
			name:     match[2],
			sql:      string(data),
			checksum: sha256.Sum256(data),
		})
	}
	if len(migrations) == 0 {
		return nil, errors.New("database: no migrations")
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	for i, m := range migrations {
		wantVersion := int64(i + 1)
		if m.version != wantVersion {
			return nil, fmt.Errorf("migration version = %06d, want %06d", m.version, wantVersion)
		}
	}

	return migrations, nil
}

func validateLedger(migrations []migration, applied []appliedMigration) error {
	if len(applied) > len(migrations) {
		return fmt.Errorf("%w: database has %d migrations but binary has %d", ErrMigrationDrift, len(applied), len(migrations))
	}
	for i, got := range applied {
		want := migrations[i]
		if got.version != want.version || got.name != want.name || got.checksum != want.checksumHex() {
			return fmt.Errorf("%w at version %06d", ErrMigrationDrift, got.version)
		}
	}
	return nil
}
