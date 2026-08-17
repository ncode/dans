package identifier

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

func TestNewUUID(t *testing.T) {
	t.Parallel()

	got, err := newUUID(bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatalf("NewUUID: %v", err)
	}
	const want = "00000000-0000-4000-8000-000000000000"
	if got != want {
		t.Errorf("NewUUID() = %q, want %q", got, want)
	}
	if err := ValidateUUID(got); err != nil {
		t.Errorf("ValidateUUID(%q): %v", got, err)
	}
}

func TestNewUUID_propagatesRandomSourceFailure(t *testing.T) {
	t.Parallel()

	if _, err := newUUID(errReader{}); err == nil {
		t.Fatal("NewUUID() error = nil, want random source error")
	}
}

func TestValidateUUID_rejectsNonCanonicalOrNonV4(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"00000000-0000-4000-8000-00000000000",
		"00000000-0000-4000-8000-00000000000G",
		"00000000-0000-4000-8000-0000000000000",
		"00000000-0000-4000-8000-00000000000A",
		"00000000-0000-3000-8000-000000000000",
		"00000000-0000-4000-0000-000000000000",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if err := ValidateUUID(input); err == nil {
				t.Errorf("ValidateUUID(%q) error = nil", input)
			}
		})
	}
}

func TestNewToken(t *testing.T) {
	t.Parallel()

	random := bytes.Repeat([]byte{0xa5}, tokenBytes)
	secret, digest, err := newToken(bytes.NewReader(random))
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	want := TokenPrefix + base64.RawURLEncoding.EncodeToString(random)
	if secret != want {
		t.Errorf("NewToken() secret = %q, want %q", secret, want)
	}
	if err := ValidateToken(secret); err != nil {
		t.Errorf("ValidateToken(%q): %v", secret, err)
	}
	if got := sha256.Sum256([]byte(secret)); digest != got {
		t.Errorf("NewToken() digest = %x, want %x", digest, got)
	}
	if strings.Contains(string(digest[:]), secret) {
		t.Fatal("stored digest contains plaintext token")
	}
}

func TestValidateToken_rejectsMalformedValues(t *testing.T) {
	t.Parallel()

	validPayload := base64.RawURLEncoding.EncodeToString(make([]byte, tokenBytes))
	tests := []string{
		"",
		validPayload,
		TokenPrefix + validPayload[:len(validPayload)-1],
		TokenPrefix + validPayload + "A",
		TokenPrefix + strings.Repeat("!", len(validPayload)),
		TokenPrefix + validPayload[:len(validPayload)-1] + "B", // non-zero trailing bits
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if err := ValidateToken(input); err == nil {
				t.Errorf("ValidateToken(%q) error = nil", input)
			}
		})
	}
}

func TestDigestToken(t *testing.T) {
	t.Parallel()

	const token = TokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	want := sha256.Sum256([]byte(token))
	if got := DigestToken(token); got != want {
		t.Errorf("DigestToken() = %x, want %x", got, want)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
