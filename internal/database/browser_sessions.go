package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ncode/dans/internal/identifier"
)

const browserSessionPrefix = "session_v1_"

// ValidBrowserSession verifies the distinct, canonical browser credential syntax.
func ValidBrowserSession(secret string) bool {
	suffix, ok := strings.CutPrefix(secret, browserSessionPrefix)
	return ok && identifier.ValidateToken(identifier.TokenPrefix+suffix) == nil
}

// CreatedBrowserSession carries the one-time secret to the cookie response only.
type CreatedBrowserSession struct {
	Secret    string `json:"-"`
	ExpiresAt time.Time
}

// CreateBrowserSession replaces only the supplied previous browser session.
func (s *Store) CreateBrowserSession(ctx context.Context, token, previous string) (CreatedBrowserSession, error) {
	if identifier.ValidateToken(token) != nil {
		return CreatedBrowserSession{}, ErrUnauthenticated
	}
	random, _, err := identifier.NewToken()
	if err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("generate browser session: %w", err)
	}
	secret := browserSessionPrefix + strings.TrimPrefix(random, identifier.TokenPrefix)
	digest := identifier.DigestToken(secret)
	tokenDigest := identifier.DigestToken(token)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("begin browser session: %w", err)
	}
	defer rollback(tx)
	q := New(tx)
	expires, err := q.CreateBrowserSession(ctx, CreateBrowserSessionParams{SessionDigest: digest[:], TokenDigest: tokenDigest[:]})
	if errors.Is(err, pgx.ErrNoRows) {
		return CreatedBrowserSession{}, ErrUnauthenticated
	}
	if err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("create browser session: %w", err)
	}
	if ValidBrowserSession(previous) {
		oldDigest := identifier.DigestToken(previous)
		if err := q.DeleteBrowserSession(ctx, oldDigest[:]); err != nil {
			return CreatedBrowserSession{}, fmt.Errorf("replace browser session: %w", err)
		}
	}
	if err := q.DeleteExpiredBrowserSessions(ctx); err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("clean expired browser sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("commit browser session: %w", err)
	}
	return CreatedBrowserSession{Secret: secret, ExpiresAt: expires.Time}, nil
}

// DeleteBrowserSession invalidates a session without changing its original token.
func (s *Store) DeleteBrowserSession(ctx context.Context, secret string) error {
	if !ValidBrowserSession(secret) {
		return nil
	}
	digest := identifier.DigestToken(secret)
	if err := s.queries.DeleteBrowserSession(ctx, digest[:]); err != nil {
		return fmt.Errorf("delete browser session: %w", err)
	}
	return nil
}
