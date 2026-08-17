package api_test

import (
	"testing"

	"github.com/ncode/dans/api"
)

func TestGeneratedClientIsPubliclyImportable(t *testing.T) {
	t.Parallel()

	client, err := api.NewClient("https://dans.example.test")
	if err != nil {
		t.Fatalf("construct generated public client: %v", err)
	}
	if client == nil {
		t.Fatal("generated public client is nil")
	}
}
