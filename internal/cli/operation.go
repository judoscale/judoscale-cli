package cli

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/judoscale/judoscale-cli/internal/spec"
)

// runOperation runs a command built from a spec operation. Path parameters are
// positional (in path order); query parameters are flags.
func (a *App) runOperation(ctx context.Context, op spec.Operation, args []string) error {
	fs := flag.NewFlagSet(commandName(op), flag.ContinueOnError)
	query := map[string]*string{}
	for _, p := range op.QueryParams {
		query[p.Name] = fs.String(p.Name, "", "")
	}
	var all *bool
	if _, paginated := query["cursor"]; paginated {
		all = fs.Bool("all", false, "")
	}

	pos, err := parseInterleaved(fs, args)
	if err != nil {
		if err == flagHelp {
			a.operationHelp(op, all != nil)
		}
		return err
	}
	if len(pos) != len(op.PathParams) {
		return usagef("usage: %s", usageLine(op))
	}

	path := op.Path
	for i, p := range op.PathParams {
		path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(pos[i]), 1)
	}
	q := url.Values{}
	for _, p := range op.QueryParams {
		v := *query[p.Name]
		if v == "" {
			if p.Required {
				return usagef("missing required flag --%s\nusage: %s", p.Name, usageLine(op))
			}
			continue
		}
		q.Set(p.Name, v)
	}

	c, err := a.newClient()
	if err != nil {
		return err
	}
	var body []byte
	if all != nil && *all {
		body, err = c.GetAll(ctx, path, q)
	} else {
		body, err = c.Do(ctx, op.Method, path, q, nil)
	}
	if err != nil {
		return err
	}
	return a.printJSON(body)
}

func usageLine(op spec.Operation) string {
	line := "judo " + commandName(op)
	for _, p := range op.PathParams {
		line += " <" + p.Name + ">"
	}
	return line + " [flags]"
}

func (a *App) operationHelp(op spec.Operation, paginated bool) {
	w := a.Stdout
	fmt.Fprintf(w, "%s\n\nUsage: %s\n", op.Summary, usageLine(op))
	if op.Description != "" {
		fmt.Fprintf(w, "\n%s\n", op.Description)
	}
	if len(op.QueryParams) == 0 && !paginated {
		return
	}
	fmt.Fprint(w, "\nFlags:\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range op.QueryParams {
		desc := p.Description
		if p.Required {
			desc += " (required)"
		}
		fmt.Fprintf(tw, "  --%s string\t%s\n", p.Name, strings.TrimSpace(desc))
	}
	if paginated {
		fmt.Fprintln(tw, "  --all\tFollow next_cursor and merge all pages")
	}
	tw.Flush()
}
