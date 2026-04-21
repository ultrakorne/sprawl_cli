package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ultrakorne/sprawl_cli/internal/cli"
)

func main() {
	// Cancel long-running commands (e.g. the device-flow poll) on ctrl+C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
