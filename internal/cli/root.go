package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/httpserver"
	"github.com/spf13/cobra"
)

// Streams are the command's standard input, output, and error streams.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Options configures one independent command tree.
type Options struct {
	Version     string
	Streams     Streams
	HTTPClient  api.HttpRequestDoer
	Maintenance Maintenance
	Server      Server
}

// NewCommand constructs a fresh Cobra command tree.
func NewCommand(options Options) *cobra.Command {
	streams := options.Streams.withDefaults()
	root := &cobra.Command{
		Use:           "dans",
		Short:         "Manage DANS and delegated DNS records",
		Version:       options.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	root.SetVersionTemplate("{{.Version}}\n")
	addConfigFlags(root)
	root.AddCommand(newVersionCommand(options.Version), newCompletionCommand(root))
	root.AddCommand(newServeCommand(options))
	root.AddCommand(newManagementCommands(options)...)
	root.AddCommand(newMaintenanceCommands(options)...)
	return root
}

// Execute runs a fresh command tree and renders any error at the process boundary.
func Execute(ctx context.Context, args []string, options Options) int {
	root := NewCommand(options)
	return executeCommand(ctx, args, root)
}

func executeCommand(ctx context.Context, args []string, root *cobra.Command) int {
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		code := exitCode(err)
		if _, structured := errors.AsType[*serveRuntimeError](err); structured {
			httpserver.NewJSONLogger(root.ErrOrStderr(), slog.LevelInfo).ErrorContext(ctx, "serve failed",
				"event", "runtime_error", "error", err.Error())
		} else {
			_, _ = fmt.Fprintln(root.ErrOrStderr(), err)
		}
		if code == 2 {
			_, _ = fmt.Fprintln(root.ErrOrStderr(), root.UsageString())
		}
		return code
	}
	return 0
}

func (streams Streams) withDefaults() Streams {
	if streams.In == nil {
		streams.In = emptyReader{}
	}
	if streams.Out == nil {
		streams.Out = io.Discard
	}
	if streams.Err == nil {
		streams.Err = io.Discard
	}
	return streams
}

type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the DANS version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
}

func exitCode(err error) int {
	if errors.Is(err, context.Canceled) {
		return 130
	}
	if classified, ok := errors.AsType[*commandError](err); ok {
		return classified.code
	}
	return 2
}

type commandError struct {
	code int
	err  error
}

func (err *commandError) Error() string { return err.err.Error() }

func (err *commandError) Unwrap() error { return err.err }

func runtimeFailure(err error) error {
	if err == nil {
		return nil
	}
	return &commandError{code: 1, err: err}
}

func invocationFailure(err error) error {
	if err == nil {
		return nil
	}
	return &commandError{code: 2, err: err}
}
