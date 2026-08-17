package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ncode/dans/internal/identifier"
)

type BootstrapInput struct {
	Handle      string
	DisplayName string
	TokenLabel  string
}

type BootstrapResult struct {
	InstallationID string
	Identity       Identity
	Token          CreatedToken
}

type OperatorRecoveryInput struct {
	IdentityID string
	Handle     string
	TokenLabel string
}

type RestoreFinalizationInput = OperatorRecoveryInput

type OperatorCredential struct {
	Identity Identity
	Token    TokenMetadata
	Secret   string
}

func (s *Store) Bootstrap(ctx context.Context, input BootstrapInput) (BootstrapResult, error) {
	if !validHandle(input.Handle) || !validHandle(input.TokenLabel) {
		return BootstrapResult{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("begin bootstrap: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	if _, err := q.LockOperatorGuard(ctx); err != nil {
		return BootstrapResult{}, fmt.Errorf("lock bootstrap guard: %w", err)
	}
	state, err := q.GetBootstrapState(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("read bootstrap state: %w", err)
	}
	if state.Installations != 0 || state.Identities != 0 {
		return BootstrapResult{}, ErrConflict
	}
	installationID, err := identifier.NewUUID()
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("generate installation id: %w", err)
	}
	identityID, err := identifier.NewUUID()
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("generate bootstrap identity id: %w", err)
	}
	tokenID, err := identifier.NewUUID()
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("generate bootstrap token id: %w", err)
	}
	secret, digest, err := identifier.NewToken()
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("generate bootstrap token: %w", err)
	}
	if _, err := q.CreateInstallationMetadata(ctx, installationID); err != nil {
		return BootstrapResult{}, fmt.Errorf("create installation metadata: %w", mapStoreError(err))
	}
	var displayName *string
	if input.DisplayName != "" {
		displayName = &input.DisplayName
	}
	identity, err := q.CreateOperatorIdentity(ctx, CreateOperatorIdentityParams{
		ID: identityID, Handle: input.Handle, DisplayName: displayName,
	})
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap identity: %w", mapStoreError(err))
	}
	row, err := q.CreateAPIToken(ctx, CreateAPITokenParams{
		ID: tokenID, IdentityID: identityID, Label: input.TokenLabel, Digest: digest[:], ExpiresAt: pgtype.Timestamptz{},
	})
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap token: %w", mapStoreError(err))
	}
	requestActor, err := newOfflineActor()
	if err != nil {
		return BootstrapResult{}, err
	}
	if err := recordManagementAudit(ctx, q, requestActor, "installation.bootstrap", "identity", identityID, map[string]any{
		"installation_id": installationID, "handle": input.Handle, "token_label": input.TokenLabel,
	}); err != nil {
		return BootstrapResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BootstrapResult{}, fmt.Errorf("commit bootstrap: %w", err)
	}
	return BootstrapResult{
		InstallationID: installationID,
		Identity:       identity,
		Token:          CreatedToken{Token: tokenMetadataFromCreate(row), Secret: secret},
	}, nil
}

func (s *Store) RecoverOperator(ctx context.Context, input OperatorRecoveryInput) (OperatorCredential, error) {
	return s.issueOfflineOperatorToken(ctx, input, "operator.recover", false)
}

// FinalizeRestore atomically invalidates restored credentials and issues one replacement.
// The supported restore procedure keeps every API instance quiesced until this commits.
func (s *Store) FinalizeRestore(ctx context.Context, input RestoreFinalizationInput) (OperatorCredential, error) {
	return s.issueOfflineOperatorToken(ctx, input, "restore.finalize", true)
}

func (s *Store) issueOfflineOperatorToken(ctx context.Context, input OperatorRecoveryInput, action string, revokeAll bool) (OperatorCredential, error) {
	if (input.IdentityID == "") == (input.Handle == "") || !validHandle(input.TokenLabel) {
		return OperatorCredential{}, ErrInvalid
	}
	if input.IdentityID != "" && identifier.ValidateUUID(input.IdentityID) != nil || input.Handle != "" && !validHandle(input.Handle) {
		return OperatorCredential{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OperatorCredential{}, fmt.Errorf("begin %s: %w", action, err)
	}
	defer rollback(tx)
	q := New(tx)
	if _, err := q.LockOperatorGuard(ctx); err != nil {
		return OperatorCredential{}, fmt.Errorf("lock operator guard for %s: %w", action, err)
	}
	identity, err := resolveOperatorForUpdate(ctx, q, input)
	if err != nil {
		return OperatorCredential{}, err
	}
	if !identity.Enabled || !identity.IsOperator {
		return OperatorCredential{}, ErrConflict
	}
	if revokeAll {
		if _, err := q.GetInstallationMetadata(ctx); err != nil {
			return OperatorCredential{}, fmt.Errorf("get installation for restore finalization: %w", mapStoreError(err))
		}
		if _, err := q.RevokeAllActiveAPITokens(ctx); err != nil {
			return OperatorCredential{}, fmt.Errorf("revoke restored tokens: %w", err)
		}
	}
	tokenID, err := identifier.NewUUID()
	if err != nil {
		return OperatorCredential{}, fmt.Errorf("generate offline token id: %w", err)
	}
	secret, digest, err := identifier.NewToken()
	if err != nil {
		return OperatorCredential{}, fmt.Errorf("generate offline token: %w", err)
	}
	row, err := q.CreateAPIToken(ctx, CreateAPITokenParams{
		ID: tokenID, IdentityID: identity.ID, Label: input.TokenLabel, Digest: digest[:], ExpiresAt: pgtype.Timestamptz{},
	})
	if err != nil {
		return OperatorCredential{}, fmt.Errorf("create offline operator token: %w", mapStoreError(err))
	}
	requestActor, err := newOfflineActor()
	if err != nil {
		return OperatorCredential{}, err
	}
	if err := recordManagementAudit(ctx, q, requestActor, action, "api_token", tokenID, map[string]any{
		"identity_id": identity.ID, "handle": identity.Handle, "token_label": input.TokenLabel,
	}); err != nil {
		return OperatorCredential{}, err
	}
	if revokeAll {
		if _, err := q.FinalizeInstallationRestore(ctx); err != nil {
			return OperatorCredential{}, fmt.Errorf("finalize installation restore: %w", mapStoreError(err))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return OperatorCredential{}, fmt.Errorf("commit %s: %w", action, err)
	}
	return OperatorCredential{Identity: identity, Token: tokenMetadataFromCreate(row), Secret: secret}, nil
}

func resolveOperatorForUpdate(ctx context.Context, q *Queries, input OperatorRecoveryInput) (Identity, error) {
	var (
		identity Identity
		err      error
	)
	if input.IdentityID != "" {
		identity, err = q.GetIdentityForUpdate(ctx, input.IdentityID)
	} else {
		identity, err = q.GetIdentityByHandleForUpdate(ctx, input.Handle)
	}
	if err != nil {
		return Identity{}, fmt.Errorf("resolve operator: %w", mapStoreError(err))
	}
	return identity, nil
}

func newOfflineActor() (Actor, error) {
	requestID, err := identifier.NewUUID()
	if err != nil {
		return Actor{}, fmt.Errorf("generate offline audit request id: %w", err)
	}
	return Actor{RequestID: requestID}, nil
}
