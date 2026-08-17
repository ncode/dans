package cli

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ncode/dans/internal/httpapi"
	"github.com/spf13/cobra"
)

// MigrationState is the stable CLI representation of database schema status.
type MigrationState struct {
	CurrentVersion int  `json:"current_version"`
	LatestVersion  int  `json:"latest_version"`
	Compatible     bool `json:"compatible"`
}

// BootstrapInput describes the first human operator and token.
type BootstrapInput struct {
	Handle      string
	DisplayName string
	TokenLabel  string
}

// OperatorRecoveryInput identifies an existing operator and the new token label.
type OperatorRecoveryInput struct {
	IdentityID string
	Handle     string
	TokenLabel string
}

// RestoreFinalizationInput selects the operator that receives the sole replacement token.
type RestoreFinalizationInput struct {
	IdentityID string
	Handle     string
	TokenLabel string
}

// OneTimeCredential is printed once after an offline credential-issuance workflow.
type OneTimeCredential struct {
	IdentityID string `json:"identity_id"`
	Handle     string `json:"handle"`
	TokenID    string `json:"token_id"`
	TokenLabel string `json:"token_label"`
	Secret     string `json:"secret"`
}

// Maintenance is the narrow adapter from Cobra commands to database application services.
type Maintenance interface {
	Migrate(context.Context, httpapi.Secret) error
	MigrationStatus(context.Context, httpapi.Secret) (MigrationState, error)
	Bootstrap(context.Context, httpapi.Secret, BootstrapInput) (OneTimeCredential, error)
	RecoverOperator(context.Context, httpapi.Secret, OperatorRecoveryInput) (OneTimeCredential, error)
	FinalizeRestore(context.Context, httpapi.Secret, RestoreFinalizationInput) (OneTimeCredential, error)
}

func newMaintenanceCommands(options Options) []*cobra.Command {
	return []*cobra.Command{
		newDatabaseCommand(options),
		newBootstrapCommand(options),
		newRecoveryCommand(options),
		newRestoreCommand(options),
	}
}

func newDatabaseCommand(options Options) *cobra.Command {
	database := &cobra.Command{Use: "db", Short: "Manage the DANS database schema"}
	database.AddCommand(
		&cobra.Command{
			Use:   "migrate",
			Short: "Apply pending database migrations",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				config, databaseURL, maintenance, err := maintenanceContext(cmd, options)
				if err != nil {
					return err
				}
				if err := maintenance.Migrate(cmd.Context(), databaseURL); err != nil {
					return maintenanceFailure("migrate database", err, databaseURL)
				}
				return emitSuccess(cmd, config)
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report database schema status",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				config, databaseURL, maintenance, err := maintenanceContext(cmd, options)
				if err != nil {
					return err
				}
				status, err := maintenance.MigrationStatus(cmd.Context(), databaseURL)
				if err != nil {
					return maintenanceFailure("read database migration status", err, databaseURL)
				}
				return emitAPIResult(cmd, config, status)
			},
		},
	)
	return database
}

func newBootstrapCommand(options Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "bootstrap",
		Short: "Create the first human operator and API token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			input, err := bootstrapFlags(cmd)
			if err != nil {
				return err
			}
			config, databaseURL, maintenance, err := maintenanceContext(cmd, options)
			if err != nil {
				return err
			}
			credential, err := maintenance.Bootstrap(cmd.Context(), databaseURL, input)
			if err != nil {
				return maintenanceFailure("bootstrap installation", err, databaseURL)
			}
			return emitAPIResult(cmd, config, credential)
		},
	}
	command.Flags().String("handle", "", "Initial operator handle")
	command.Flags().String("display-name", "", "Initial operator display name")
	command.Flags().String("token-label", "", "Initial API token label")
	return command
}

func newRecoveryCommand(options Options) *cobra.Command {
	recoverCommand := &cobra.Command{Use: "recover", Short: "Recover offline operator access"}
	operatorToken := &cobra.Command{
		Use:   "operator-token",
		Short: "Issue a token for an existing enabled operator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			selector, err := operatorSelector(cmd)
			if err != nil {
				return err
			}
			config, databaseURL, maintenance, err := maintenanceContext(cmd, options)
			if err != nil {
				return err
			}
			credential, err := maintenance.RecoverOperator(cmd.Context(), databaseURL, OperatorRecoveryInput{
				IdentityID: selector.IdentityID,
				Handle:     selector.Handle,
				TokenLabel: selector.TokenLabel,
			})
			if err != nil {
				return maintenanceFailure("recover operator token", err, databaseURL)
			}
			return emitAPIResult(cmd, config, credential)
		},
	}
	addOperatorSelectorFlags(operatorToken)
	recoverCommand.AddCommand(operatorToken)
	return recoverCommand
}

func newRestoreCommand(options Options) *cobra.Command {
	restore := &cobra.Command{Use: "restore", Short: "Finalize a restored installation"}
	finalize := &cobra.Command{
		Use:   "finalize",
		Short: "Revoke restored tokens and issue one replacement",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireConfirmation(cmd); err != nil {
				return err
			}
			selector, err := operatorSelector(cmd)
			if err != nil {
				return err
			}
			config, databaseURL, maintenance, err := maintenanceContext(cmd, options)
			if err != nil {
				return err
			}
			credential, err := maintenance.FinalizeRestore(cmd.Context(), databaseURL, RestoreFinalizationInput{
				IdentityID: selector.IdentityID,
				Handle:     selector.Handle,
				TokenLabel: selector.TokenLabel,
			})
			if err != nil {
				return maintenanceFailure("finalize restore", err, databaseURL)
			}
			return emitAPIResult(cmd, config, credential)
		},
	}
	addOperatorSelectorFlags(finalize)
	addConfirmationFlag(finalize)
	restore.AddCommand(finalize)
	return restore
}

func maintenanceContext(cmd *cobra.Command, options Options) (Config, httpapi.Secret, Maintenance, error) {
	config, err := loadConfig(cmd, configScopeDatabase)
	if err != nil {
		return Config{}, httpapi.Secret{}, nil, invocationFailure(err)
	}
	databaseURL, err := loadDatabaseURL(config)
	if err != nil {
		return Config{}, httpapi.Secret{}, nil, invocationFailure(err)
	}
	if options.Maintenance == nil {
		return Config{}, httpapi.Secret{}, nil, runtimeFailure(fmt.Errorf("database maintenance is not available"))
	}
	return config, databaseURL, options.Maintenance, nil
}

func bootstrapFlags(cmd *cobra.Command) (BootstrapInput, error) {
	handle, err := requiredStringFlag(cmd, "handle")
	if err != nil {
		return BootstrapInput{}, err
	}
	displayName, err := cmd.Flags().GetString("display-name")
	if err != nil {
		return BootstrapInput{}, invocationFailure(fmt.Errorf("read --display-name: %w", err))
	}
	tokenLabel, err := requiredStringFlag(cmd, "token-label")
	if err != nil {
		return BootstrapInput{}, err
	}
	return BootstrapInput{Handle: handle, DisplayName: displayName, TokenLabel: tokenLabel}, nil
}

type operatorSelection struct {
	IdentityID string
	Handle     string
	TokenLabel string
}

func addOperatorSelectorFlags(command *cobra.Command) {
	command.Flags().String("identity-id", "", "Existing operator resource ID")
	command.Flags().String("handle", "", "Existing operator handle")
	command.Flags().String("token-label", "", "Replacement API token label")
}

func operatorSelector(cmd *cobra.Command) (operatorSelection, error) {
	identityID, err := cmd.Flags().GetString("identity-id")
	if err != nil {
		return operatorSelection{}, invocationFailure(fmt.Errorf("read --identity-id: %w", err))
	}
	handle, err := cmd.Flags().GetString("handle")
	if err != nil {
		return operatorSelection{}, invocationFailure(fmt.Errorf("read --handle: %w", err))
	}
	if (identityID == "") == (handle == "") {
		return operatorSelection{}, invocationFailure(fmt.Errorf("exactly one of --identity-id or --handle is required"))
	}
	if identityID != "" {
		if _, err := parseResourceID(identityID); err != nil {
			return operatorSelection{}, err
		}
	}
	tokenLabel, err := requiredStringFlag(cmd, "token-label")
	if err != nil {
		return operatorSelection{}, err
	}
	return operatorSelection{IdentityID: identityID, Handle: handle, TokenLabel: tokenLabel}, nil
}

func requiredStringFlag(cmd *cobra.Command, name string) (string, error) {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		return "", invocationFailure(fmt.Errorf("read --%s: %w", name, err))
	}
	if value == "" {
		return "", invocationFailure(fmt.Errorf("--%s is required", name))
	}
	return value, nil
}

func maintenanceFailure(operation string, err error, databaseURL httpapi.Secret) error {
	secrets := []httpapi.Secret{databaseURL}
	if parsed, parseErr := url.Parse(databaseURL.Value()); parseErr == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			secrets = append(secrets, httpapi.NewSecret(password))
		}
	}
	return runtimeFailure(fmt.Errorf("%s: %w", operation, &redactedCause{
		cause:   err,
		message: httpapi.RedactText(err.Error(), secrets...),
	}))
}

type redactedCause struct {
	cause   error
	message string
}

func (err *redactedCause) Error() string { return err.message }
func (err *redactedCause) Unwrap() error { return err.cause }
