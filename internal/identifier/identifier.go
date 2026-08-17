// Package identifier creates and validates DANS resource and credential IDs.
package identifier

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	// TokenPrefix identifies the only token format supported by this release.
	TokenPrefix = "dans_v1_"
	tokenBytes  = 32
	tokenLength = len(TokenPrefix) + 43
)

var (
	// ErrInvalidUUID reports a resource ID that is not a canonical lowercase UUIDv4.
	ErrInvalidUUID = errors.New("identifier: invalid UUIDv4")
	// ErrInvalidToken reports a credential that is not in the supported token format.
	ErrInvalidToken = errors.New("identifier: invalid API token")
)

// NewUUID returns a lowercase UUIDv4 generated with cryptographic randomness.
func NewUUID() (string, error) {
	return newUUID(rand.Reader)
}

func newUUID(random io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(random, value[:]); err != nil {
		return "", fmt.Errorf("read UUID randomness: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

// ValidateUUID verifies the canonical lowercase UUIDv4 resource-ID format.
func ValidateUUID(value string) error {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return ErrInvalidUUID
	}
	for i, c := range []byte(value) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !isLowerHex(c) {
			return ErrInvalidUUID
		}
	}
	if value[14] != '4' || (value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b') {
		return ErrInvalidUUID
	}
	return nil
}

func isLowerHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
}

// NewToken returns a one-time plaintext token and the digest suitable for storage.
func NewToken() (string, [sha256.Size]byte, error) {
	return newToken(rand.Reader)
}

func newToken(random io.Reader) (string, [sha256.Size]byte, error) {
	var value [tokenBytes]byte
	if _, err := io.ReadFull(random, value[:]); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("read token randomness: %w", err)
	}
	secret := TokenPrefix + base64.RawURLEncoding.EncodeToString(value[:])
	return secret, DigestToken(secret), nil
}

// ValidateToken verifies exact syntax and canonical unpadded base64url encoding.
func ValidateToken(value string) error {
	if len(value) != tokenLength || value[:len(TokenPrefix)] != TokenPrefix {
		return ErrInvalidToken
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value[len(TokenPrefix):])
	if err != nil || len(decoded) != tokenBytes {
		return ErrInvalidToken
	}
	return nil
}

// DigestToken returns the indexed lookup digest for the complete token value.
func DigestToken(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}
