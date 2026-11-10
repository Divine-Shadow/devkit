package main

import (
	"context"
	"os"
	"os/signal"

	"devkit/cli/devctl/internal/fleetmode"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(fleetmode.Main(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
