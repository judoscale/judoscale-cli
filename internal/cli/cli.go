// Package cli wires the core client and the OpenAPI spec into commands.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/judoscale/judoscale-cli/internal/client"
	"github.com/judoscale/judoscale-cli/internal/spec"
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
	case "update":
		return a.runUpdate(ctx, rest)
	case "auth":
		return a.runAuth(ctx, rest)
	}

	s, err := a.loadSpec()
	if err != nil {
		return err
	}
	g := findGroup(commandTree(s), name)
	if g == nil {
		return usagef("unknown command %q; run `judo help` to list commands", name)
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		a.groupHelp(g)
		return nil
	}
	if c := g.find(rest[0]); c != nil {
		return a.runOperation(ctx, *c, rest[1:])
	}
	return usagef("unknown command %q; run `judo %s` to list its commands", name+" "+rest[0], name)
}

// appDir is where the CLI keeps its files: the cached spec and credentials.
func (a *App) appDir() string {
	if a.ConfigDir == "" {
		return ""
	}
	return filepath.Join(a.ConfigDir, "judoscale")
}

func (a *App) loadSpec() (*spec.Spec, error) {
	s, warning, err := spec.Load(a.appDir(), a.Version)
	if warning != "" {
		fmt.Fprintln(a.Stderr, "Warning:", warning)
	}
	return s, err
}

func (a *App) baseURL() string {
	if u := a.Getenv("JUDOSCALE_API_URL"); u != "" {
		return u
	}
	return client.DefaultBaseURL
}

func (a *App) newClient() (*client.Client, error) {
	token, _, err := a.token()
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("no API token: set JUDOSCALE_API_TOKEN or run `judo auth login`")
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
	w := a.Stdout
	fmt.Fprint(w, `judo: command line client for Judoscale

Usage: judo <resource> <command> [arguments] [flags]

Environment:
  JUDOSCALE_API_TOKEN  API token (overrides the one saved by judo auth login)
  JUDOSCALE_API_URL    API base URL (default https://api.judoscale.com)
`)

	general := []helpEntry{
		{"auth", "Log in with an API token: auth login|logout|status"},
		{"api", "Make a raw API request: api <METHOD> <path>"},
		{"update", "Download the latest API spec and refresh the list of available commands"},
		{"version", "Print the CLI version"},
	}
	var resources []helpEntry
	if s, err := a.loadSpec(); err == nil {
		for _, g := range commandTree(s) {
			resources = append(resources, helpEntry{g.name, firstSentence(g.description)})
		}
	}
	printEntries(w, "General", general, resources)
	if len(resources) > 0 {
		printEntries(w, "Resources", resources, general)
	}
	fmt.Fprintln(w, "\nRun `judo <resource>` to list its commands, and `judo <resource> <command> --help` for details.")
	return nil
}

func (a *App) groupHelp(g *group) {
	w := a.Stdout
	if g.description != "" {
		fmt.Fprintf(w, "%s\n\n", strings.TrimSpace(g.description))
	}
	fmt.Fprintf(w, "Usage: judo %s <command> [arguments] [flags]\n", g.name)
	var entries []helpEntry
	for _, c := range g.commands {
		entries = append(entries, helpEntry{c.name, c.op.Summary})
	}
	printEntries(w, "Commands", entries)
	fmt.Fprintf(w, "\nRun `judo %s <command> --help` for details.\n", g.name)
}

type helpEntry struct{ name, summary string }

// printEntries prints a titled list, aligned with the other lists given.
func printEntries(w io.Writer, title string, entries []helpEntry, alignWith ...[]helpEntry) {
	width := 0
	for _, list := range append(alignWith, entries) {
		for _, e := range list {
			width = max(width, len(e.name))
		}
	}
	fmt.Fprintf(w, "\n%s:\n", title)
	for _, e := range entries {
		fmt.Fprintf(w, "  %-*s  %s\n", width, e.name, e.summary)
	}
}

// firstSentence shortens a description to its first sentence, on one line.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}

// kebab converts an operationId like getProcessMetricSeries to get-process-metric-series.
func kebab(id string) string {
	var b strings.Builder
	for i, r := range id {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
