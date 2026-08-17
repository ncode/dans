package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestWriteResultIsDeterministicForTextAndJSON(t *testing.T) {
	type result struct {
		ID     string `json:"id"`
		Handle string `json:"handle"`
	}
	value := result{ID: "id-1", Handle: "alice"}

	var textOutput bytes.Buffer
	if err := writeResult(&textOutput, "text", "id-1\talice", value); err != nil {
		t.Fatalf("write text result: %v", err)
	}
	if got, want := textOutput.String(), "id-1\talice\n"; got != want {
		t.Fatalf("text output = %q, want %q", got, want)
	}

	var jsonOutput bytes.Buffer
	if err := writeResult(&jsonOutput, "json", "ignored", value); err != nil {
		t.Fatalf("write JSON result: %v", err)
	}
	if got, want := jsonOutput.String(), "{\"id\":\"id-1\",\"handle\":\"alice\"}\n"; got != want {
		t.Fatalf("JSON output = %q, want %q", got, want)
	}
}

func TestWriteNDJSONWritesOneCompleteObjectPerLine(t *testing.T) {
	var output bytes.Buffer
	if err := writeNDJSON(&output,
		map[string]string{"id": "event-1"},
		map[string]string{"id": "event-2"},
	); err != nil {
		t.Fatalf("write NDJSON: %v", err)
	}
	if got, want := output.String(), "{\"id\":\"event-1\"}\n{\"id\":\"event-2\"}\n"; got != want {
		t.Fatalf("NDJSON = %q, want %q", got, want)
	}
	if strings.HasPrefix(output.String(), "[") {
		t.Fatal("NDJSON was wrapped in an array")
	}
}

func TestExecuteCommandClassifiesRuntimeAndCancellation(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		run      func(*cobra.Command, []string) error
		wantCode int
		want     string
	}{
		{
			name: "runtime",
			ctx:  context.Background(),
			run: func(*cobra.Command, []string) error {
				return runtimeFailure(errors.New("remote unavailable"))
			},
			wantCode: 1,
			want:     "remote unavailable\n",
		},
		{
			name: "canceled",
			ctx:  canceledContext(),
			run: func(cmd *cobra.Command, _ []string) error {
				return cmd.Context().Err()
			},
			wantCode: 130,
			want:     "context canceled\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := &cobra.Command{Use: "dans", SilenceErrors: true, SilenceUsage: true, RunE: tt.run}
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			if code := executeCommand(tt.ctx, nil, root); code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", code, tt.wantCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if got := stderr.String(); got != tt.want {
				t.Fatalf("stderr = %q, want %q", got, tt.want)
			}
			if strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("runtime diagnostic included usage: %q", stderr.String())
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
