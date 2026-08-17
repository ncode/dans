package cli

import (
	"testing"

	"github.com/ncode/dans/internal/database"
)

func TestNewDatabaseMaintenanceSatisfiesCommandPort(t *testing.T) {
	t.Parallel()

	var maintenance Maintenance = NewDatabaseMaintenance()
	if maintenance == nil {
		t.Fatal("NewDatabaseMaintenance returned nil")
	}
}

func TestDatabaseMaintenanceMapsApplicationResults(t *testing.T) {
	t.Parallel()

	bootstrap := bootstrapCredential(database.BootstrapResult{
		Identity: database.Identity{ID: "identity-1", Handle: "alice"},
		Token: database.CreatedToken{
			Token:  database.TokenMetadata{ID: "token-1", Label: "initial"},
			Secret: "dans_v1_bootstrap",
		},
	})
	if bootstrap != (OneTimeCredential{
		IdentityID: "identity-1",
		Handle:     "alice",
		TokenID:    "token-1",
		TokenLabel: "initial",
		Secret:     "dans_v1_bootstrap",
	}) {
		t.Fatalf("bootstrap credential = %#v", bootstrap)
	}

	recovered := operatorCredential(database.OperatorCredential{
		Identity: database.Identity{ID: "identity-2", Handle: "bob"},
		Token:    database.TokenMetadata{ID: "token-2", Label: "recovered"},
		Secret:   "dans_v1_recovered",
	})
	if recovered != (OneTimeCredential{
		IdentityID: "identity-2",
		Handle:     "bob",
		TokenID:    "token-2",
		TokenLabel: "recovered",
		Secret:     "dans_v1_recovered",
	}) {
		t.Fatalf("operator credential = %#v", recovered)
	}

	status := migrationState(database.SchemaStatus{CurrentVersion: 3, LatestVersion: 4, Compatible: true})
	if status != (MigrationState{CurrentVersion: 3, LatestVersion: 4, Compatible: true}) {
		t.Fatalf("migration state = %#v", status)
	}
}
