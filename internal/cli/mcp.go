package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/judoscale/judoscale-cli/internal/mcp"
	"github.com/judoscale/judoscale-cli/internal/spec"
)

const mcpHelp = `Run a Model Context Protocol server on stdin and stdout, so AI agents can use the Judoscale API.

Usage: judo mcp [--writes]

Every API operation is a tool, built from the same spec as the other commands
and named after its operationId in snake_case (list_apps, update_process).
Only read operations are exposed unless --writes is given.

Add it to an agent, for example: claude mcp add judoscale -- judo mcp
It uses the token from JUDOSCALE_API_TOKEN or judo auth login.
`

func (a *App) runMCP(ctx context.Context, args []string) error {
	writes := false
	for _, arg := range args {
		switch arg {
		case "--writes":
			writes = true
		case "-h", "--help":
			fmt.Fprint(a.Stdout, mcpHelp)
			return nil
		default:
			return usagef("usage: judo mcp [--writes]")
		}
	}

	s, err := a.loadSpec()
	if err != nil {
		return err
	}
	return a.mcpServer(s, writes).Serve(ctx, a.Stdin, a.Stdout)
}

func (a *App) mcpServer(s *spec.Spec, writes bool) *mcp.Server {
	commands := map[string]command{}
	var tools []mcp.Tool
	hidden := 0
	for _, g := range commandTree(s) {
		for _, c := range g.commands {
			if !writes && c.op.Method != http.MethodGet {
				hidden++
				continue
			}
			t := mcpTool(c)
			commands[t.Name] = c
			tools = append(tools, t)
		}
	}

	instructions := "Tools are the Judoscale Team API's operations."
	if hidden > 0 {
		instructions += fmt.Sprintf(" Read-only mode: %d write operations (create, update, delete) are not available. The user can restart the server with `judo mcp --writes` to enable them.", hidden)
	}
	return &mcp.Server{
		Name:         "judoscale",
		Version:      a.Version,
		Instructions: instructions,
		Tools:        tools,
		Call: func(ctx context.Context, name string, args map[string]any) (string, error) {
			return a.mcpCall(ctx, commands[name], args)
		},
	}
}

func mcpTool(c command) mcp.Tool {
	op := c.op
	props := map[string]any{}
	var required []string
	add := func(p spec.Param, isRequired bool) {
		if _, taken := props[p.Name]; taken {
			return // a path or query param wins over a body field of the same name
		}
		props[p.Name] = paramSchema(p)
		if isRequired {
			required = append(required, p.Name)
		}
	}
	for _, p := range op.PathParams {
		add(p, true)
	}
	for _, p := range op.QueryParams {
		add(p, p.Required)
	}
	for _, p := range op.BodyParams {
		add(p, p.Required)
	}

	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}

	desc := op.Summary
	if op.Description != "" {
		desc += "\n\n" + strings.TrimSpace(op.Description)
	}
	return mcp.Tool{
		Name:        strings.ReplaceAll(kebab(op.ID), "-", "_"),
		Description: desc,
		InputSchema: schema,
		Annotations: &mcp.Annotations{
			ReadOnlyHint:    op.Method == http.MethodGet,
			DestructiveHint: destructive(op),
		},
	}
}

// paramSchema is the JSON schema for a parameter, with the fields of objects
// (or of array items) one level deep.
func paramSchema(p spec.Param) map[string]any {
	typ := p.Type
	if typ == "" {
		typ = "string"
	}
	sc := map[string]any{"type": typ}
	if p.Description != "" {
		sc["description"] = strings.TrimSpace(p.Description)
	}
	if len(p.Enum) > 0 && typ == "string" {
		sc["enum"] = p.Enum
	}
	if len(p.Fields) == 0 {
		return sc
	}

	props := map[string]any{}
	var required []string
	for _, f := range p.Fields {
		props[f.Name] = paramSchema(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	object := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		object["required"] = required
	}
	if typ == "array" {
		sc["items"] = object
	} else {
		sc["properties"] = props
		if len(required) > 0 {
			sc["required"] = required
		}
	}
	return sc
}

// mcpCall sends the request for a tool call, like the command for the same
// operation does. Path and query values come from arguments by name, and the
// remaining arguments that are body fields become the JSON body.
func (a *App) mcpCall(ctx context.Context, cmd command, args map[string]any) (string, error) {
	op := cmd.op
	used := map[string]bool{}

	path := op.Path
	for _, p := range op.PathParams {
		v, ok := args[p.Name]
		if !ok {
			return "", fmt.Errorf("missing required argument %q", p.Name)
		}
		path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(fmt.Sprint(v)), 1)
		used[p.Name] = true
	}

	q := url.Values{}
	for _, p := range op.QueryParams {
		v, ok := args[p.Name]
		if !ok {
			if p.Required {
				return "", fmt.Errorf("missing required argument %q", p.Name)
			}
			continue
		}
		q.Set(p.Name, fmt.Sprint(v))
		used[p.Name] = true
	}

	body := map[string]any{}
	for _, p := range op.BodyParams {
		if v, ok := args[p.Name]; ok && !used[p.Name] {
			body[p.Name] = v
			used[p.Name] = true
		}
	}
	for name := range args {
		if !used[name] {
			return "", fmt.Errorf("unknown argument %q", name)
		}
	}
	var payload []byte
	if len(body) > 0 {
		payload, _ = json.Marshal(body)
	}

	c, err := a.newClient()
	if err != nil {
		return "", err
	}
	resp, err := c.Do(ctx, op.Method, path, q, payload)
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(resp)) == 0 {
		return "Done (no content)", nil
	}
	var compact bytes.Buffer
	if json.Compact(&compact, resp) != nil {
		return string(resp), nil
	}
	return compact.String(), nil
}
