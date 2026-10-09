package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ultrakorne/sprawl_cli/internal/cli"
)

func main() {
	// Cancel long-running commands (e.g. the device-flow poll) on ctrl+C. The
	// first signal only cancels; once it has, a second one gets the default
	// behaviour and kills the process, in case something can't wind down.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
