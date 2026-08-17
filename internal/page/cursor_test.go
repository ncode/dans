package page

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	want := Key{
		CreatedAt: time.Date(2026, time.August, 14, 10, 11, 12, 345, time.FixedZone("offset", 2*60*60)),
		ID:        "00000000-0000-4000-8000-000000000000",
	}
	encoded, err := EncodeCursor("identities", "enabled=true", want)
	if err != nil {
		t.Fatalf("EncodeCursor: %v", err)
	}
	got, err := DecodeCursor(encoded, "identities", "enabled=true")
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	want.CreatedAt = want.CreatedAt.UTC()
	if !got.CreatedAt.Equal(want.CreatedAt) || got.CreatedAt.Location() != time.UTC || got.ID != want.ID {
		t.Errorf("DecodeCursor() = %+v, want %+v", got, want)
	}
}

func TestDecodeCursorRejectsMalformedOrMismatchedValues(t *testing.T) {
	t.Parallel()

	key := Key{
		CreatedAt: time.Date(2026, time.August, 14, 10, 11, 12, 0, time.UTC),
		ID:        "00000000-0000-4000-8000-000000000000",
	}
	valid, err := EncodeCursor("identities", "enabled=true", key)
	if err != nil {
		t.Fatalf("EncodeCursor: %v", err)
	}
	tests := []struct {
		name     string
		cursor   string
		resource string
		filters  string
	}{
		{name: "empty", cursor: "", resource: "identities", filters: "enabled=true"},
		{name: "bad base64", cursor: "%%%", resource: "identities", filters: "enabled=true"},
		{name: "overlong", cursor: strings.Repeat("A", MaxCursorLength+1), resource: "identities", filters: "enabled=true"},
		{name: "wrong resource", cursor: valid, resource: "groups", filters: "enabled=true"},
		{name: "wrong filters", cursor: valid, resource: "identities", filters: "enabled=false"},
		{name: "unknown field", cursor: rawCursor(t, `{"v":1,"r":"identities","f":"x","t":"2026-08-14T10:11:12Z","i":"00000000-0000-4000-8000-000000000000","extra":true}`), resource: "identities", filters: "enabled=true"},
		{name: "duplicate field", cursor: rawCursor(t, `{"v":1,"v":1,"r":"identities","f":"x","t":"2026-08-14T10:11:12Z","i":"00000000-0000-4000-8000-000000000000"}`), resource: "identities", filters: "enabled=true"},
		{name: "wrong version", cursor: rawCursor(t, `{"v":2,"r":"identities","f":"x","t":"2026-08-14T10:11:12Z","i":"00000000-0000-4000-8000-000000000000"}`), resource: "identities", filters: "enabled=true"},
		{name: "invalid time", cursor: rawCursor(t, `{"v":1,"r":"identities","f":"x","t":"not-time","i":"00000000-0000-4000-8000-000000000000"}`), resource: "identities", filters: "enabled=true"},
		{name: "invalid ID", cursor: rawCursor(t, `{"v":1,"r":"identities","f":"x","t":"2026-08-14T10:11:12Z","i":"not-a-uuid"}`), resource: "identities", filters: "enabled=true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeCursor(tt.cursor, tt.resource, tt.filters); !errors.Is(err, ErrInvalidCursor) {
				t.Errorf("DecodeCursor() error = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

func TestCursorDoesNotPromiseACollectionSnapshot(t *testing.T) {
	t.Parallel()

	after := Key{
		CreatedAt: time.Date(2026, time.August, 14, 10, 11, 12, 0, time.UTC),
		ID:        "00000000-0000-4000-8000-000000000000",
	}
	cursor, err := EncodeCursor("audit", "actor_id=abc", after)
	if err != nil {
		t.Fatalf("EncodeCursor: %v", err)
	}

	// A new row may be committed here. The cursor remains only a keyset position;
	// request-time authentication, visibility, and collection state are rechecked by the caller.
	got, err := DecodeCursor(cursor, "audit", "actor_id=abc")
	if err != nil {
		t.Fatalf("DecodeCursor after collection change: %v", err)
	}
	if got != after {
		t.Errorf("DecodeCursor() = %+v, want %+v", got, after)
	}
}

func TestNormalizeLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   int
		want    int
		wantErr bool
	}{
		{input: 0, want: DefaultLimit},
		{input: 1, want: 1},
		{input: MaxLimit, want: MaxLimit},
		{input: -1, wantErr: true},
		{input: MaxLimit + 1, wantErr: true},
	}
	for _, tt := range tests {
		got, err := NormalizeLimit(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("NormalizeLimit(%d) error = %v, wantErr %t", tt.input, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("NormalizeLimit(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func rawCursor(t *testing.T, value string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}
