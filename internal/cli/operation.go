package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/judoscale/judoscale-cli/internal/spec"
)

// runOperation runs a command built from a spec operation. Path parameters are
// positional (in path order); query parameters and body fields are flags.
func (a *App) runOperation(ctx context.Context, cmd command, args []string) error {
	op := cmd.op
	fs := flag.NewFlagSet(cmd.fullName(), flag.ContinueOnError)
	query := map[string]*string{}
	for _, p := range op.QueryParams {
		query[p.Name] = fs.String(p.Name, "", "")
	}
	var all *bool
	if _, paginated := query["cursor"]; paginated {
		all = fs.Bool("all", false, "")
	}
	var yes *bool
	if destructive(op) {
		yes = fs.Bool("yes", false, "")
	}
	var data *string
	if hasBody(op) {
		data = fs.String("data", "", "")
		fs.StringVar(data, "d", "", "")
	}
	fields := map[string]any{} // *string or *bool
	for _, p := range op.BodyParams {
		if fs.Lookup(p.Name) != nil {
			continue // name taken by another flag; still settable via --data
		}
		if p.Type == "boolean" {
			fields[p.Name] = fs.Bool(p.Name, false, "")
		} else {
			fields[p.Name] = fs.String(p.Name, "", "")
		}
	}

	pos, err := parseInterleaved(fs, args)
	if err != nil {
		if err == flagHelp {
			a.operationHelp(cmd, all != nil)
		}
		return err
	}
	if len(pos) != len(op.PathParams) {
		return usagef("usage: %s", usageLine(cmd))
	}

	pathValues := map[string]string{}
	for i, p := range op.PathParams {
		pathValues[p.Name] = pos[i]
	}
	queryValues := map[string]string{}
	for _, p := range op.QueryParams {
		queryValues[p.Name] = *query[p.Name]
	}
	path, q, missing := buildRequest(op, pathValues, queryValues)
	if missing != "" {
		return usagef("missing required flag --%s\nusage: %s", missing, usageLine(cmd))
	}

	if yes != nil && !*yes {
		return usagef("%s is destructive; pass --yes to confirm", cmd.fullName())
	}

	body, err := a.requestBody(op, fs, data, fields)
	if err != nil {
		return err
	}

	c, err := a.newClient()
	if err != nil {
		return err
	}
	var resp []byte
	if all != nil && *all {
		resp, err = c.GetAll(ctx, path, q)
	} else {
		resp, err = c.Do(ctx, op.Method, path, q, body)
	}
	if err != nil {
		return err
	}
	return a.printJSON(resp)
}

// buildRequest expands the operation's path and builds its query from values
// by parameter name; empty query values are left out. It returns the name of
// the first required parameter that has no value, if any.
func buildRequest(op spec.Operation, pathValues, queryValues map[string]string) (path string, q url.Values, missing string) {
	path = op.Path
	for _, p := range op.PathParams {
		v, ok := pathValues[p.Name]
		if !ok {
			return "", nil, p.Name
		}
		path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(v), 1)
	}
	q = url.Values{}
	for _, p := range op.QueryParams {
		v := queryValues[p.Name]
		if v == "" {
			if p.Required {
				return "", nil, p.Name
			}
			continue
		}
		q.Set(p.Name, v)
	}
	return path, q, ""
}

// notDestructive lists DELETE operations that don't need --yes.
var notDestructive = map[string]bool{
	"disableAppMaintenance": true,
}

// destructive reports whether op needs --yes to run.
func destructive(op spec.Operation) bool {
	return op.Method == http.MethodDelete && !notDestructive[op.ID]
}

func hasBody(op spec.Operation) bool {
	switch op.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	}
	return false
}

// requestBody builds the JSON body from --data, or from the body field flags
// that were set. Fields are encoded by their schema type.
func (a *App) requestBody(op spec.Operation, fs *flag.FlagSet, data *string, fields map[string]any) ([]byte, error) {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	body := map[string]json.RawMessage{}
	for _, p := range op.BodyParams {
		v, ok := fields[p.Name]
		if !ok || !set[p.Name] {
			continue
		}
		if b, isBool := v.(*bool); isBool {
			body[p.Name] = json.RawMessage(strconv.FormatBool(*b))
			continue
		}
		raw, err := encodeField(p, *v.(*string))
		if err != nil {
			return nil, err
		}
		body[p.Name] = raw
	}

	if data == nil || *data == "" {
		if len(body) == 0 {
			return nil, nil
		}
		return json.Marshal(body)
	}
	if len(body) > 0 {
		return nil, usagef("use either --data or field flags, not both")
	}
	if *data != "-" {
		return []byte(*data), nil
	}
	return io.ReadAll(a.Stdin)
}

func encodeField(p spec.Param, v string) (json.RawMessage, error) {
	switch p.Type {
	case "string":
		return json.Marshal(v)
	case "integer":
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return nil, usagef("--%s must be an integer", p.Name)
		}
	case "number":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return nil, usagef("--%s must be a number", p.Name)
		}
	default:
		if !json.Valid([]byte(v)) {
			return nil, usagef("--%s must be JSON", p.Name)
		}
	}
	return json.RawMessage(v), nil
}

func usageLine(cmd command) string {
	line := "judo " + cmd.fullName()
	for _, p := range cmd.op.PathParams {
		line += " <" + p.Name + ">"
	}
	return line + " [flags]"
}

// flagType is the value placeholder shown in help for a body field.
func flagType(t string) string {
	switch t {
	case "string":
		return " string"
	case "integer":
		return " int"
	case "number":
		return " number"
	case "boolean":
		return ""
	}
	return " json"
}

func (a *App) operationHelp(cmd command, paginated bool) {
	op := cmd.op
	w := a.Stdout
	fmt.Fprintf(w, "%s\n\nUsage: %s\n", op.Summary, usageLine(cmd))
	if op.Description != "" {
		fmt.Fprintf(w, "\n%s\n", op.Description)
	}
	if len(op.QueryParams) == 0 && len(op.BodyParams) == 0 && !paginated && !hasBody(op) && !destructive(op) {
		return
	}
	fmt.Fprint(w, "\nFlags:\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	describe := func(p spec.Param) string {
		desc := strings.TrimSuffix(firstLine(strings.TrimSpace(p.Description)), ":")
		if len(p.Enum) > 0 {
			desc += " (one of: " + strings.Join(p.Enum, ", ") + ")"
		}
		if p.Required {
			desc += " (required)"
		}
		return strings.TrimSpace(desc)
	}
	for _, p := range op.QueryParams {
		fmt.Fprintf(tw, "  --%s string\t%s\n", p.Name, describe(p))
	}
	for _, p := range op.BodyParams {
		fmt.Fprintf(tw, "  --%s%s\t%s\n", p.Name, flagType(p.Type), describe(p))
		for _, f := range p.Fields {
			typ := flagType(f.Type)
			if f.Type == "boolean" {
				typ = " bool" // JSON values, not flags
			}
			fmt.Fprintf(tw, "      %s%s\t%s\n", f.Name, typ, describe(f))
		}
	}
	if hasBody(op) {
		fmt.Fprintln(tw, "  -d, --data string\tJSON request body instead of field flags; use - to read it from stdin")
	}
	if destructive(op) {
		fmt.Fprintln(tw, "  --yes\tConfirm this destructive action (required)")
	}
	if paginated {
		fmt.Fprintln(tw, "  --all\tFollow next_cursor and merge all pages")
	}
	tw.Flush()
	if slices.ContainsFunc(op.BodyParams, func(p spec.Param) bool { return p.Type == "boolean" }) {
		fmt.Fprintln(w, "\nBoolean flags are set with --name or --name=false.")
	}
}

// firstLine keeps help rows to one line; full descriptions are in the API docs.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
