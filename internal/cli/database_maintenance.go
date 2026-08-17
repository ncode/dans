package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpapi"
)

// DatabaseMaintenance adapts the offline Cobra workflows to PostgreSQL
// application services. Each command owns exactly one short-lived connection.
type DatabaseMaintenance struct{}

// NewDatabaseMaintenance constructs the production offline-maintenance adapter.
func NewDatabaseMaintenance() *DatabaseMaintenance { return &DatabaseMaintenance{} }

func (*DatabaseMaintenance) Migrate(ctx context.Context, databaseURL httpapi.Secret) error {
	_, err := withDatabaseConnection(ctx, databaseURL, func(conn *pgx.Conn) (struct{}, error) {
		return struct{}{}, database.Migrate(ctx, conn)
	})
	return err
}

func (*DatabaseMaintenance) MigrationStatus(ctx context.Context, databaseURL httpapi.Secret) (MigrationState, error) {
	status, err := withDatabaseConnection(ctx, databaseURL, func(conn *pgx.Conn) (database.SchemaStatus, error) {
		return database.MigrationStatus(ctx, conn)
	})
	return migrationState(status), err
}

func (*DatabaseMaintenance) Bootstrap(ctx context.Context, databaseURL httpapi.Secret, input BootstrapInput) (OneTimeCredential, error) {
	result, err := withDatabaseConnection(ctx, databaseURL, func(conn *pgx.Conn) (database.BootstrapResult, error) {
		return database.NewStore(conn).Bootstrap(ctx, database.BootstrapInput{
			Handle: input.Handle, DisplayName: input.DisplayName, TokenLabel: input.TokenLabel,
		})
	})
	return bootstrapCredential(result), err
}

func (*DatabaseMaintenance) RecoverOperator(ctx context.Context, databaseURL httpapi.Secret, input OperatorRecoveryInput) (OneTimeCredential, error) {
	result, err := withDatabaseConnection(ctx, databaseURL, func(conn *pgx.Conn) (database.OperatorCredential, error) {
		return database.NewStore(conn).RecoverOperator(ctx, database.OperatorRecoveryInput{
			IdentityID: input.IdentityID, Handle: input.Handle, TokenLabel: input.TokenLabel,
		})
	})
	return operatorCredential(result), err
}

func (*DatabaseMaintenance) FinalizeRestore(ctx context.Context, databaseURL httpapi.Secret, input RestoreFinalizationInput) (OneTimeCredential, error) {
	result, err := withDatabaseConnection(ctx, databaseURL, func(conn *pgx.Conn) (database.OperatorCredential, error) {
		return database.NewStore(conn).FinalizeRestore(ctx, database.RestoreFinalizationInput{
			IdentityID: input.IdentityID, Handle: input.Handle, TokenLabel: input.TokenLabel,
		})
	})
	return operatorCredential(result), err
}

func withDatabaseConnection[T any](ctx context.Context, databaseURL httpapi.Secret, action func(*pgx.Conn) (T, error)) (result T, err error) {
	conn, err := pgx.Connect(ctx, databaseURL.Value())
	if err != nil {
		return result, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(context.WithoutCancel(ctx)); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close PostgreSQL: %w", closeErr))
		}
	}()
	return action(conn)
}

func migrationState(status database.SchemaStatus) MigrationState {
	return MigrationState{
		CurrentVersion: status.CurrentVersion,
		LatestVersion:  status.LatestVersion,
		Compatible:     status.Compatible,
	}
}

func bootstrapCredential(result database.BootstrapResult) OneTimeCredential {
	return OneTimeCredential{
		IdentityID: result.Identity.ID,
		Handle:     result.Identity.Handle,
		TokenID:    result.Token.Token.ID,
		TokenLabel: result.Token.Token.Label,
		Secret:     result.Token.Secret,
	}
}

func operatorCredential(result database.OperatorCredential) OneTimeCredential {
	return OneTimeCredential{
		IdentityID: result.Identity.ID,
		Handle:     result.Identity.Handle,
		TokenID:    result.Token.ID,
		TokenLabel: result.Token.Label,
		Secret:     result.Secret,
	}
}
