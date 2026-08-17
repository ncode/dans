package database

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestLoadMigrations(t *testing.T) {
	t.Parallel()

	filesystem := fstest.MapFS{
		"migrations/000002_second.sql": &fstest.MapFile{Data: []byte("SELECT 2;\n")},
		"migrations/000001_first.sql":  &fstest.MapFile{Data: []byte("SELECT 1;\n")},
	}

	migrations, err := loadMigrations(filesystem)
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("len(migrations) = %d, want 2", len(migrations))
	}
	if migrations[0].version != 1 || migrations[0].name != "first" {
		t.Errorf("migration[0] = version %d name %q, want version 1 name %q", migrations[0].version, migrations[0].name, "first")
	}
	if migrations[1].version != 2 || migrations[1].name != "second" {
		t.Errorf("migration[1] = version %d name %q, want version 2 name %q", migrations[1].version, migrations[1].name, "second")
	}
	wantChecksum := sha256.Sum256(filesystem["migrations/000001_first.sql"].Data)
	if migrations[0].checksum != wantChecksum {
		t.Errorf("migration[0].checksum = %x, want %x", migrations[0].checksum, wantChecksum)
	}
}

func TestLoadMigrations_rejectsInvalidSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fs.FS
	}{
		{
			name: "invalid filename",
			filesystem: fstest.MapFS{
				"migrations/1_initial.sql": &fstest.MapFile{Data: []byte("SELECT 1;\n")},
			},
		},
		{
			name: "duplicate version",
			filesystem: fstest.MapFS{
				"migrations/000001_first.sql":  &fstest.MapFile{Data: []byte("SELECT 1;\n")},
				"migrations/000001_second.sql": &fstest.MapFile{Data: []byte("SELECT 2;\n")},
			},
		},
		{
			name: "version gap",
			filesystem: fstest.MapFS{
				"migrations/000001_first.sql": &fstest.MapFile{Data: []byte("SELECT 1;\n")},
				"migrations/000003_third.sql": &fstest.MapFile{Data: []byte("SELECT 3;\n")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := loadMigrations(tt.filesystem); err == nil {
				t.Fatal("loadMigrations() error = nil, want error")
			}
		})
	}
}

func TestValidateLedger_rejectsDrift(t *testing.T) {
	t.Parallel()

	migrations, err := loadMigrations(fstest.MapFS{
		"migrations/000001_initial.sql": &fstest.MapFile{Data: []byte("SELECT 1;\n")},
	})
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}

	tests := []struct {
		name    string
		applied []appliedMigration
	}{
		{
			name: "checksum changed",
			applied: []appliedMigration{{
				version:  1,
				name:     "initial",
				checksum: "changed",
			}},
		},
		{
			name: "name changed",
			applied: []appliedMigration{{
				version:  1,
				name:     "renamed",
				checksum: migrations[0].checksumHex(),
			}},
		},
		{
			name: "unknown applied version",
			applied: []appliedMigration{{
				version:  2,
				name:     "future",
				checksum: "unknown",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := validateLedger(migrations, tt.applied); !errors.Is(err, ErrMigrationDrift) {
				t.Fatalf("validateLedger() error = %v, want %v", err, ErrMigrationDrift)
			}
		})
	}
}
