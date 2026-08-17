// Package page implements bounded keyset-pagination cursors.
package page

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/ncode/dans/internal/identifier"
)

const (
	cursorVersion   = 1
	DefaultLimit    = 100
	MaxLimit        = 500
	MaxCursorLength = 512
	maxResourceLen  = 64
)

var (
	// ErrInvalidCursor reports malformed, mismatched, or unsupported cursor input.
	ErrInvalidCursor = errors.New("page: invalid cursor")
	// ErrInvalidLimit reports a collection limit outside the supported range.
	ErrInvalidLimit = errors.New("page: invalid limit")
)

// Key is the descending created-at/resource-ID keyset position.
type Key struct {
	CreatedAt time.Time
	ID        string
}

type cursorPayload struct {
	Version  int    `json:"v"`
	Resource string `json:"r"`
	Filters  string `json:"f"`
	Time     string `json:"t"`
	ID       string `json:"i"`
}

// EncodeCursor encodes a versioned cursor bound to a resource and filter set.
func EncodeCursor(resource, filters string, key Key) (string, error) {
	if !validResource(resource) || key.CreatedAt.IsZero() || identifier.ValidateUUID(key.ID) != nil {
		return "", ErrInvalidCursor
	}
	payload := cursorPayload{
		Version:  cursorVersion,
		Resource: resource,
		Filters:  filterDigest(resource, filters),
		Time:     key.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:       key.ID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", ErrInvalidCursor
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > MaxCursorLength {
		return "", ErrInvalidCursor
	}
	return encoded, nil
}

// DecodeCursor verifies a cursor for the current resource and exact filters.
func DecodeCursor(value, resource, filters string) (Key, error) {
	if value == "" || len(value) > MaxCursorLength || !validResource(resource) {
		return Key{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return Key{}, ErrInvalidCursor
	}
	var payload cursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Key{}, ErrInvalidCursor
	}
	canonical, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Key{}, ErrInvalidCursor
	}
	if payload.Version != cursorVersion || payload.Resource != resource || payload.Filters != filterDigest(resource, filters) {
		return Key{}, ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.Time)
	if err != nil || createdAt.IsZero() || createdAt.Location() != time.UTC || createdAt.Format(time.RFC3339Nano) != payload.Time {
		return Key{}, ErrInvalidCursor
	}
	if identifier.ValidateUUID(payload.ID) != nil {
		return Key{}, ErrInvalidCursor
	}
	return Key{CreatedAt: createdAt, ID: payload.ID}, nil
}

// NormalizeLimit applies the documented default and maximum collection size.
func NormalizeLimit(value int) (int, error) {
	if value == 0 {
		return DefaultLimit, nil
	}
	if value < 1 || value > MaxLimit {
		return 0, ErrInvalidLimit
	}
	return value, nil
}

func filterDigest(resource, filters string) string {
	digest := sha256.Sum256([]byte(resource + "\x00" + filters))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func validResource(value string) bool {
	if len(value) == 0 || len(value) > maxResourceLen {
		return false
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
