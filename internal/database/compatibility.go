package database

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrDatabaseUnavailable     = errors.New("database: unavailable")
	ErrUnsupportedPostgreSQL   = errors.New("database: unsupported PostgreSQL version")
	ErrReadOnlyDatabase        = errors.New("database: primary is not writable")
	ErrIncompatibleSchema      = errors.New("database: incompatible schema")
	ErrAuditUnavailable        = errors.New("database: audit storage unavailable")
	ErrInstallationUnavailable = errors.New("database: installation unavailable")
)

type runtimeQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CheckRuntimeCompatibility verifies all durable readiness invariants without
// creating or migrating schema.
func CheckRuntimeCompatibility(ctx context.Context, database runtimeQueryRower) error {
	if database == nil {
		return ErrDatabaseUnavailable
	}
	migrations, err := loadMigrations(embeddedMigrations)
	if err != nil {
		return ErrIncompatibleSchema
	}
	versions := make([]int64, len(migrations))
	names := make([]string, len(migrations))
	checksums := make([]string, len(migrations))
	for index, migration := range migrations {
		versions[index] = migration.version
		names[index] = migration.name
		checksums[index] = migration.checksumHex()
	}

	var major int
	var writable, schemaCompatible, auditWritable, installationReady bool
	err = database.QueryRow(ctx, `
WITH expected(version, name, checksum) AS (
    SELECT * FROM unnest($1::bigint[], $2::text[], $3::text[])
), schema_exact AS (
    SELECT
        (SELECT count(*) FROM schema_migrations) = (SELECT count(*) FROM expected)
        AND NOT EXISTS (
            SELECT 1
            FROM expected e
            LEFT JOIN schema_migrations a
              ON a.version = e.version AND a.name = e.name AND a.checksum = e.checksum
            WHERE a.version IS NULL
        ) AS compatible
)
SELECT
    current_setting('server_version_num')::integer / 10000,
    NOT pg_is_in_recovery() AND current_setting('transaction_read_only') = 'off',
    schema_exact.compatible,
    has_table_privilege(current_user, 'audit_events', 'INSERT'),
    COALESCE((SELECT count(*) = 1 AND bool_and(NOT restore_required) FROM installation_metadata), false)
FROM schema_exact`, versions, names, checksums).Scan(&major, &writable, &schemaCompatible, &auditWritable, &installationReady)
	if err != nil {
		if databaseError, ok := errors.AsType[*pgconn.PgError](err); ok && databaseError.Code == "42P01" {
			return ErrIncompatibleSchema
		}
		return ErrDatabaseUnavailable
	}
	if major < 16 || major > 18 {
		return ErrUnsupportedPostgreSQL
	}
	if !writable {
		return ErrReadOnlyDatabase
	}
	if !schemaCompatible {
		return ErrIncompatibleSchema
	}
	if !auditWritable {
		return ErrAuditUnavailable
	}
	if !installationReady {
		return ErrInstallationUnavailable
	}
	return nil
}
