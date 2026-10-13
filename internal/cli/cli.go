// Package cli wires the core client and the OpenAPI spec into commands.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
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
	}

	s, err := a.loadSpec()
	if err != nil {
		return err
	}
	if op := findCommand(s, name); op != nil {
		return a.runOperation(ctx, *op, rest)
	}
	return usagef("unknown command %q; run `judo help` to list commands", name)
}

func (a *App) specDir() string {
	if a.ConfigDir == "" {
		return ""
	}
	return filepath.Join(a.ConfigDir, "judoscale")
}

func (a *App) loadSpec() (*spec.Spec, error) {
	s, warning, err := spec.Load(a.specDir(), a.Version)
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
	w := a.Stdout
	fmt.Fprint(w, `judo: command line client for the Judoscale Team API

Usage: judo <command> [arguments] [flags]

Environment:
  JUDOSCALE_API_TOKEN  API token (required for API commands)
  JUDOSCALE_API_URL    API base URL (default https://api.judoscale.com)
`)

	groups := []helpGroup{{"General", []helpEntry{
		{"api", "Make a raw API request: api <METHOD> <path>"},
		{"version", "Print the CLI version"},
	}}}
	if s, err := a.loadSpec(); err == nil {
		groups = append(groups, groupByTag(s)...)
	}

	width := 0
	for _, g := range groups {
		for _, e := range g.entries {
			width = max(width, len(e.name))
		}
	}
	for _, g := range groups {
		fmt.Fprintf(w, "\n%s:\n", g.title)
		for _, e := range g.entries {
			fmt.Fprintf(w, "  %-*s  %s\n", width, e.name, e.summary)
		}
	}
	fmt.Fprintln(w, "\nRun `judo <command> --help` for details.")
	return nil
}

type helpEntry struct{ name, summary string }

type helpGroup struct {
	title   string
	entries []helpEntry
}

// groupByTag sections the commands by operation tag, in the spec's tag order.
// Tags the spec doesn't declare follow alphabetically, then untagged commands.
func groupByTag(s *spec.Spec) []helpGroup {
	byTag := map[string][]helpEntry{}
	for _, op := range commandOps(s) {
		byTag[op.Tag] = append(byTag[op.Tag], helpEntry{commandName(op), op.Summary})
	}

	var order []string
	for _, t := range s.Tags {
		if _, ok := byTag[t]; ok && t != "" {
			order = append(order, t)
		}
	}
	var rest []string
	for t := range byTag {
		if t != "" && !slices.Contains(order, t) {
			rest = append(rest, t)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)
	if _, ok := byTag[""]; ok {
		order = append(order, "")
	}

	groups := make([]helpGroup, 0, len(order))
	for _, t := range order {
		title := t
		if title == "" {
			title = "Other"
		}
		groups = append(groups, helpGroup{title, byTag[t]})
	}
	return groups
}

func commandName(op spec.Operation) string { return kebab(op.ID) }

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

// commandOps returns the operations exposed as commands: read-only ones.
func commandOps(s *spec.Spec) []spec.Operation {
	var ops []spec.Operation
	for _, op := range s.Operations {
		if op.Method == http.MethodGet {
			ops = append(ops, op)
		}
	}
	sort.Slice(ops, func(i, j int) bool { return commandName(ops[i]) < commandName(ops[j]) })
	return ops
}

func findCommand(s *spec.Spec, name string) *spec.Operation {
	for _, op := range commandOps(s) {
		if commandName(op) == name {
			return &op
		}
	}
	return nil
}
