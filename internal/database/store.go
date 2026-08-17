package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ncode/dans/internal/identifier"
)

var (
	// ErrUnauthenticated reports an unusable credential without disclosing why.
	ErrUnauthenticated = errors.New("database: unauthenticated")
	// ErrForbidden reports an authenticated actor lacking current authority.
	ErrForbidden = errors.New("database: forbidden")
	// ErrNotFound reports a resource that is not visible or does not exist.
	ErrNotFound = errors.New("database: not found")
	// ErrConflict reports a request conflicting with current durable state.
	ErrConflict = errors.New("database: conflict")
	// ErrInvalid reports invalid domain input.
	ErrInvalid = errors.New("database: invalid input")
)

type transactor interface {
	DBTX
	Begin(context.Context) (pgx.Tx, error)
}

// Store owns DANS persistence transactions and invariants.
type Store struct {
	db      transactor
	queries *Queries
}

// NewStore returns a persistence store backed by a PostgreSQL connection or pool.
func NewStore(db transactor) *Store {
	return &Store{db: db, queries: New(db)}
}

// Actor is the current authenticated identity and token at one decision point.
type Actor struct {
	IdentityID string
	TokenID    string
	Kind       string
	Handle     string
	Operator   bool
	RequestID  string
}

// Authenticate resolves one syntactically valid opaque token in one statement.
func (s *Store) Authenticate(ctx context.Context, token string) (Actor, error) {
	if err := identifier.ValidateToken(token); err != nil {
		return Actor{}, ErrUnauthenticated
	}
	digest := identifier.DigestToken(token)
	row, err := s.queries.AuthenticateToken(ctx, digest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrUnauthenticated
	}
	if err != nil {
		return Actor{}, fmt.Errorf("authenticate token: %w", err)
	}
	return Actor{
		IdentityID: row.IdentityID,
		TokenID:    row.TokenID,
		Kind:       row.Kind,
		Handle:     row.Handle,
		Operator:   row.IsOperator,
	}, nil
}

func requireOperator(actor Actor) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	if !actor.Operator {
		return ErrForbidden
	}
	return nil
}

func requireActor(actor Actor) error {
	if identifier.ValidateUUID(actor.IdentityID) != nil || identifier.ValidateUUID(actor.TokenID) != nil || actor.RequestID == "" {
		return ErrInvalid
	}
	return nil
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "23505":
			return ErrConflict
		case "23503":
			return ErrNotFound
		case "22001", "22P02", "23502", "23514":
			return ErrInvalid
		}
	}
	return err
}
