package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ncode/dans/internal/identifier"
	"github.com/ncode/dans/internal/page"
)

type TokenCreate struct {
	Label     string
	ExpiresAt *time.Time
}

// TokenMetadata deliberately excludes the credential digest and plaintext.
type TokenMetadata struct {
	ID         string
	IdentityID string
	Label      string
	Status     string
	ExpiresAt  pgtype.Timestamptz
	RevokedAt  pgtype.Timestamptz
	CreatedAt  pgtype.Timestamptz
}

type CreatedToken struct {
	Token  TokenMetadata
	Secret string
}

type TokenListOptions struct {
	Limit int
	After *page.Key
}

type TokenPage struct {
	Items []TokenMetadata
	Next  *page.Key
}

func (s *Store) CreateToken(ctx context.Context, actor Actor, identityID string, input TokenCreate) (CreatedToken, error) {
	if err := requireOwnOrOperator(actor, identityID); err != nil {
		return CreatedToken{}, err
	}
	if !validHandle(input.Label) {
		return CreatedToken{}, ErrInvalid
	}
	var expiresAt pgtype.Timestamptz
	if input.ExpiresAt != nil {
		if !input.ExpiresAt.After(time.Now()) {
			return CreatedToken{}, ErrInvalid
		}
		expiresAt = pgtype.Timestamptz{Time: input.ExpiresAt.UTC(), Valid: true}
	}
	id, err := identifier.NewUUID()
	if err != nil {
		return CreatedToken{}, fmt.Errorf("generate token id: %w", err)
	}
	secret, digest, err := identifier.NewToken()
	if err != nil {
		return CreatedToken{}, fmt.Errorf("generate token: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedToken{}, fmt.Errorf("begin create token: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	row, err := q.CreateAPIToken(ctx, CreateAPITokenParams{
		ID: id, IdentityID: identityID, Label: input.Label, Digest: digest[:], ExpiresAt: expiresAt,
	})
	if err != nil {
		return CreatedToken{}, fmt.Errorf("create token: %w", mapStoreError(err))
	}
	metadata := tokenMetadataFromCreate(row)
	if err := recordManagementAudit(ctx, q, actor, "token.create", "api_token", id, map[string]any{
		"identity_id": identityID, "label": input.Label, "expires": expiresAt.Valid,
	}); err != nil {
		return CreatedToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedToken{}, fmt.Errorf("commit create token: %w", err)
	}
	return CreatedToken{Token: metadata, Secret: secret}, nil
}

func (s *Store) ListTokens(ctx context.Context, actor Actor, identityID string, options TokenListOptions) (TokenPage, error) {
	if err := requireOwnOrOperator(actor, identityID); err != nil {
		return TokenPage{}, err
	}
	limit, err := page.NormalizeLimit(options.Limit)
	if err != nil {
		return TokenPage{}, ErrInvalid
	}
	if _, err := s.queries.GetIdentityByID(ctx, identityID); err != nil {
		return TokenPage{}, fmt.Errorf("get token owner: %w", mapStoreError(err))
	}
	params := ListAPITokensParams{IdentityID: identityID, RowLimit: int32(limit + 1)}
	if options.After != nil {
		if options.After.CreatedAt.IsZero() || identifier.ValidateUUID(options.After.ID) != nil {
			return TokenPage{}, ErrInvalid
		}
		params.AfterCreatedAt = pgtype.Timestamptz{Time: options.After.CreatedAt, Valid: true}
		params.AfterID = options.After.ID
	}
	rows, err := s.queries.ListAPITokens(ctx, params)
	if err != nil {
		return TokenPage{}, fmt.Errorf("list tokens: %w", err)
	}
	items := make([]TokenMetadata, len(rows))
	for i := range rows {
		items[i] = tokenMetadataFromList(rows[i])
	}
	result := TokenPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		result.Items = items[:limit]
		result.Next = &page.Key{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	return result, nil
}

func (s *Store) RevokeToken(ctx context.Context, actor Actor, identityID, tokenID string) error {
	if err := requireOwnOrOperator(actor, identityID); err != nil {
		return err
	}
	if identifier.ValidateUUID(tokenID) != nil {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke token: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	_, err = q.RevokeAPIToken(ctx, RevokeAPITokenParams{IdentityID: identityID, ID: tokenID})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := q.GetAPITokenMetadata(ctx, GetAPITokenMetadataParams{IdentityID: identityID, ID: tokenID}); getErr != nil {
			return fmt.Errorf("get token for idempotent revoke: %w", mapStoreError(getErr))
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("revoke token: %w", mapStoreError(err))
	}
	if err := recordManagementAudit(ctx, q, actor, "token.revoke", "api_token", tokenID, map[string]any{"identity_id": identityID}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke token: %w", err)
	}
	return nil
}

func requireOwnOrOperator(actor Actor, identityID string) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	if identifier.ValidateUUID(identityID) != nil {
		return ErrInvalid
	}
	if !actor.Operator && actor.IdentityID != identityID {
		return ErrForbidden
	}
	return nil
}

func tokenMetadataFromCreate(row CreateAPITokenRow) TokenMetadata {
	return TokenMetadata{
		ID: row.ID, IdentityID: row.IdentityID, Label: row.Label, Status: row.Status,
		ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt, CreatedAt: row.CreatedAt,
	}
}

func tokenMetadataFromList(row ListAPITokensRow) TokenMetadata {
	return TokenMetadata{
		ID: row.ID, IdentityID: row.IdentityID, Label: row.Label, Status: row.Status,
		ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt, CreatedAt: row.CreatedAt,
	}
}
