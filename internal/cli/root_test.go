package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNewCommandReturnsFreshTree(t *testing.T) {
	first := NewCommand(Options{Version: "v1.2.3", Streams: discardStreams()})
	second := NewCommand(Options{Version: "v1.2.3", Streams: discardStreams()})

	if first == second {
		t.Fatal("NewCommand returned the same root command")
	}
	if first.Commands()[0] == second.Commands()[0] {
		t.Fatal("NewCommand reused a child command")
	}

	first.SetArgs([]string{"--help"})
	if err := first.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute first command: %v", err)
	}
	second.InitDefaultHelpFlag()
	if second.Flags().Lookup("help").Changed {
		t.Fatal("executing one command tree changed another")
	}
}

func TestExecuteOfflineCommandsUseInjectedStreams(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "help", args: []string{"help"}, want: "Usage:"},
		{name: "version", args: []string{"version"}, want: "v1.2.3\n"},
		{name: "completion", args: []string{"completion", "bash"}, want: "bash completion"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), tt.args, Options{
				Version: "v1.2.3",
				Streams: Streams{Out: &stdout, Err: &stderr},
			})
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("stdout = %q, want substring %q", stdout.String(), tt.want)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestExecuteRendersInvocationErrorsOnceWithUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"not-a-command"}, Options{
		Version: "v1.2.3",
		Streams: Streams{Out: &stdout, Err: &stderr},
	})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := strings.Count(stderr.String(), "unknown command"); got != 1 {
		t.Fatalf("unknown-command error rendered %d times; stderr = %q", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func discardStreams() Streams {
	return Streams{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
}
