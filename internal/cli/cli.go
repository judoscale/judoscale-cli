// Package cli wires the core client and the OpenAPI spec into commands.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/judoscale/judoscale-cli/internal/client"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

type App struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	// ConfigDir is the user config dir; the spec cache lives in <ConfigDir>/judoscale.
	ConfigDir string
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

func (a *App) Run(ctx context.Context, args []string) int {
	err := a.run(ctx, args)
	if err == nil || errors.Is(err, flagHelp) {
		return exitOK
	}
	fmt.Fprintln(a.Stderr, "Error:", err)
	if errors.As(err, &usageError{}) {
		return exitUsage
	}
	return exitError
}

func (a *App) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.rootHelp()
	}
	name, rest := args[0], args[1:]
	switch name {
	case "help", "-h", "--help":
		return a.rootHelp()
	case "version", "--version":
		fmt.Fprintln(a.Stdout, "judo", a.Version)
		return nil
	case "api":
		return a.runAPI(ctx, rest)
	}
	return usagef("unknown command %q; run `judo help` to list commands", name)
}

func (a *App) baseURL() string {
	if u := a.Getenv("JUDOSCALE_API_URL"); u != "" {
		return u
	}
	return client.DefaultBaseURL
}

func (a *App) newClient() (*client.Client, error) {
	token := a.Getenv("JUDOSCALE_API_TOKEN")
	if token == "" {
		return nil, errors.New("JUDOSCALE_API_TOKEN is not set")
	}
	c := client.New(a.baseURL(), token)
	c.UserAgent = "judoscale-cli/" + a.Version
	c.OnRetry = func(wait time.Duration) {
		fmt.Fprintf(a.Stderr, "Rate limited; retrying in %s\n", wait)
	}
	return c, nil
}

// printJSON pretty-prints a JSON response body.
func (a *App) printJSON(body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		_, werr := a.Stdout.Write(body)
		return werr
	}
	buf.WriteByte('\n')
	_, err := a.Stdout.Write(buf.Bytes())
	return err
}

func (a *App) rootHelp() error {
	fmt.Fprint(a.Stdout, `judo: command line client for the Judoscale Team API

Usage: judo <command> [arguments] [flags]

Environment:
  JUDOSCALE_API_TOKEN  API token (required for API commands)
  JUDOSCALE_API_URL    API base URL (default https://api.judoscale.com)

Commands:
  api      Make a raw API request: api <METHOD> <path>
  version  Print the CLI version

Run `+"`judo <command> --help`"+` for details.
`)
	return nil
}
