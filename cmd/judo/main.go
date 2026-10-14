package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/judoscale/judoscale-cli/internal/cli"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	app := &cli.App{
		Version:   version,
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Getenv:    os.Getenv,
		ConfigDir: cli.ConfigDir(),
	}
	os.Exit(app.Run(ctx, os.Args[1:]))
}
