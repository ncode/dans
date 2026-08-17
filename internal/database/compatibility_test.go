package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestCheckRuntimeCompatibilityRequiresSupportedPrimaryExactSchemaAndAudit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		major             int
		writable          bool
		schemaCompatible  bool
		auditWritable     bool
		installationReady bool
		queryErr          error
		wantErr           error
	}{
		{name: "postgres 16", major: 16, writable: true, schemaCompatible: true, auditWritable: true, installationReady: true},
		{name: "postgres 17", major: 17, writable: true, schemaCompatible: true, auditWritable: true, installationReady: true},
		{name: "postgres 18", major: 18, writable: true, schemaCompatible: true, auditWritable: true, installationReady: true},
		{name: "too old", major: 15, writable: true, schemaCompatible: true, auditWritable: true, installationReady: true, wantErr: ErrUnsupportedPostgreSQL},
		{name: "too new", major: 19, writable: true, schemaCompatible: true, auditWritable: true, installationReady: true, wantErr: ErrUnsupportedPostgreSQL},
		{name: "read replica", major: 17, schemaCompatible: true, auditWritable: true, installationReady: true, wantErr: ErrReadOnlyDatabase},
		{name: "older schema", major: 17, writable: true, auditWritable: true, installationReady: true, wantErr: ErrIncompatibleSchema},
		{name: "newer schema", major: 17, writable: true, auditWritable: true, installationReady: true, wantErr: ErrIncompatibleSchema},
		{name: "audit denied", major: 17, writable: true, schemaCompatible: true, installationReady: true, wantErr: ErrAuditUnavailable},
		{name: "uninitialized or restore pending", major: 17, writable: true, schemaCompatible: true, auditWritable: true, wantErr: ErrInstallationUnavailable},
		{name: "query failure", queryErr: errors.New("database target secret"), wantErr: ErrDatabaseUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var query string
			var arguments []any
			database := queryRowerFunc(func(_ context.Context, sql string, args ...any) pgx.Row {
				query = sql
				arguments = append([]any(nil), args...)
				return rowScannerFunc(func(destinations ...any) error {
					if tt.queryErr != nil {
						return tt.queryErr
					}
					*destinations[0].(*int) = tt.major
					*destinations[1].(*bool) = tt.writable
					*destinations[2].(*bool) = tt.schemaCompatible
					*destinations[3].(*bool) = tt.auditWritable
					*destinations[4].(*bool) = tt.installationReady
					return nil
				})
			})
			err := CheckRuntimeCompatibility(t.Context(), database)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CheckRuntimeCompatibility error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(errString(err), "database target secret") {
				t.Errorf("error exposed connection detail: %v", err)
			}
			if !strings.Contains(query, "schema_migrations") || !strings.Contains(query, "has_table_privilege") || len(arguments) != 3 {
				t.Errorf("compatibility query/arguments incomplete: %q %#v", query, arguments)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type rowScannerFunc func(...any) error

func (function rowScannerFunc) Scan(destinations ...any) error { return function(destinations...) }

type queryRowerFunc func(context.Context, string, ...any) pgx.Row

func (function queryRowerFunc) QueryRow(ctx context.Context, query string, arguments ...any) pgx.Row {
	return function(ctx, query, arguments...)
}
