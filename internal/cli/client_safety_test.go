package cli

import (
	"net/http"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/spf13/cobra"
)

var _ func(*cobra.Command, Options) (Config, *api.DANSClientWithResponses, error) = onlineClient
var _ func(*cobra.Command, Options, bool) (Config, *api.DANSClientWithResponses, error) = rootAPIClient

func TestClientDoerUsesConfiguredTimeout(t *testing.T) {
	t.Parallel()

	doer, err := clientDoer(Config{RequestTimeout: "7s"}, nil)
	if err != nil {
		t.Fatalf("clientDoer() error = %v", err)
	}
	client, ok := doer.(*http.Client)
	if !ok {
		t.Fatalf("clientDoer() = %T, want *http.Client", doer)
	}
	if client.Timeout != 7*time.Second {
		t.Errorf("HTTP timeout = %s, want 7s", client.Timeout)
	}
}

func TestClientDoerPreservesOverride(t *testing.T) {
	t.Parallel()

	override := &http.Client{Timeout: time.Second}
	doer, err := clientDoer(Config{RequestTimeout: "7s"}, override)
	if err != nil {
		t.Fatalf("clientDoer() error = %v", err)
	}
	if doer != override {
		t.Fatalf("clientDoer() = %p, want caller override %p", doer, override)
	}
}
