package main

import (
	"context"
	"os"

	"github.com/judoscale/judoscale-cli/internal/cli"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	app := &cli.App{
		Version:   version,
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Getenv:    os.Getenv,
		ConfigDir: cli.ConfigDir(),
	}
	if info, err := os.Stderr.Stat(); err == nil {
		app.Interactive = info.Mode()&os.ModeCharDevice != 0
	}
	os.Exit(app.Run(context.Background(), os.Args[1:]))
}
