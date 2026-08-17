package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ncode/dans/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cli.Execute(ctx, os.Args[1:], cli.Options{
		Version:     version,
		Streams:     cli.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr},
		Maintenance: cli.NewDatabaseMaintenance(),
		Server:      cli.NewRuntimeServer(os.Stderr),
	}))
}
