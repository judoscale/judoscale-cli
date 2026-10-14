// Package spec loads the Team API OpenAPI document that commands are built from.
package spec

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

//go:embed openapi.json
var Snapshot []byte

const cacheFile = "openapi.json"

type Spec struct {
	Operations []Operation
	// Tags are the spec's declared tags, in order.
	Tags []Tag
}

type Tag struct {
	Name        string
	Description string
}

type Operation struct {
	ID          string
	Method      string // upper case
	Path        string
	Summary     string
	Description string
	// Tag is the operation's first tag, or "" if it has none.
	Tag string
	// PathParams are in the order they appear in Path.
	PathParams  []Param
	QueryParams []Param
	// BodyParams are the top-level properties of the JSON request body.
	BodyParams []Param
}

type Param struct {
	Name        string
	Description string
	Required    bool
	Type        string // JSON schema type, e.g. string, integer, boolean, array
	// Fields are the properties of an object, or of an array's object items.
	// Only one level deep, for help output.
	Fields []Param
	// Enum lists the allowed values, if the schema restricts them.
	Enum []string
}

type rawParam struct {
	Ref         string `json:"$ref"`
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Schema      struct {
		Type schemaType `json:"type"`
		Enum []any      `json:"enum"`
	} `json:"schema"`
}

type rawSchema struct {
	Ref         string               `json:"$ref"`
	Type        schemaType           `json:"type"`
	Description string               `json:"description"`
	Properties  map[string]rawSchema `json:"properties"`
	Required    []string             `json:"required"`
	Items       *rawSchema           `json:"items"`
	Enum        []any                `json:"enum"`
}

// schemaType is a JSON schema type, which may be a list such as
// ["string", "null"]; it keeps the first type that isn't null.
type schemaType string

func (t *schemaType) UnmarshalJSON(b []byte) error {
	var types []string
	if err := json.Unmarshal(b, &types); err != nil {
		var one string
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		types = []string{one}
	}
	for _, typ := range types {
		if typ != "null" {
			*t = schemaType(typ)
			return nil
		}
	}
	return nil
}

type rawOperation struct {
	OperationID string     `json:"operationId"`
	Tags        []string   `json:"tags"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	Parameters  []rawParam `json:"parameters"`
	RequestBody struct {
		Content map[string]struct {
			Schema rawSchema `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
}

type rawSpec struct {
	MinCLIVersion string `json:"x-min-cli-version"`
	Paths         map[string]map[string]json.RawMessage
	Tags          []Tag `json:"tags"`
	Components    struct {
		Parameters map[string]rawParam  `json:"parameters"`
		Schemas    map[string]rawSchema `json:"schemas"`
	} `json:"components"`
}

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
var pathVar = regexp.MustCompile(`\{([^}]+)\}`)

// Parse reads an OpenAPI JSON document. Operations without an operationId are
// skipped. cliVersion is checked against x-min-cli-version ("dev" skips it).
func Parse(data []byte, cliVersion string) (*Spec, error) {
	var raw rawSpec
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid OpenAPI document: %w", err)
	}
	if err := checkVersion(raw.MinCLIVersion, cliVersion); err != nil {
		return nil, err
	}

	s := &Spec{}
	s.Tags = raw.Tags
	seen := map[string]bool{}
	for path, item := range raw.Paths {
		var shared []rawParam
		if b, ok := item["parameters"]; ok {
			if err := json.Unmarshal(b, &shared); err != nil {
				return nil, fmt.Errorf("%s: invalid parameters: %w", path, err)
			}
		}
		for _, m := range httpMethods {
			b, ok := item[m]
			if !ok {
				continue
			}
			var op rawOperation
			if err := json.Unmarshal(b, &op); err != nil {
				return nil, fmt.Errorf("%s %s: %w", strings.ToUpper(m), path, err)
			}
			if op.OperationID == "" {
				continue
			}
			if seen[op.OperationID] {
				return nil, fmt.Errorf("duplicate operationId %q", op.OperationID)
			}
			seen[op.OperationID] = true
			s.Operations = append(s.Operations, buildOperation(op, strings.ToUpper(m), path, shared, raw))
		}
	}
	sort.Slice(s.Operations, func(i, j int) bool { return s.Operations[i].ID < s.Operations[j].ID })
	return s, nil
}

func buildOperation(op rawOperation, method, path string, shared []rawParam, raw rawSpec) Operation {
	o := Operation{ID: op.OperationID, Method: method, Path: path, Summary: op.Summary, Description: op.Description}
	if len(op.Tags) > 0 {
		o.Tag = op.Tags[0]
	}

	byKey := map[string]rawParam{}
	var keys []string
	for _, p := range append(append([]rawParam{}, shared...), op.Parameters...) {
		if p.Ref != "" {
			p = raw.Components.Parameters[strings.TrimPrefix(p.Ref, "#/components/parameters/")]
		}
		key := p.In + ":" + p.Name
		if _, dup := byKey[key]; !dup {
			keys = append(keys, key)
		}
		byKey[key] = p // operation-level overrides path-level
	}

	for _, m := range pathVar.FindAllStringSubmatch(path, -1) {
		p := byKey["path:"+m[1]]
		o.PathParams = append(o.PathParams, Param{Name: m[1], Description: p.Description, Required: true, Type: string(p.Schema.Type), Enum: enum(p.Schema.Enum)})
	}
	sort.Strings(keys)
	for _, k := range keys {
		if p := byKey[k]; p.In == "query" {
			o.QueryParams = append(o.QueryParams, Param{Name: p.Name, Description: p.Description, Required: p.Required, Type: string(p.Schema.Type), Enum: enum(p.Schema.Enum)})
		}
	}

	body := op.RequestBody.Content["application/json"].Schema
	o.BodyParams = properties(body, raw.Components.Schemas, true)
	return o
}

func enum(values []any) []string {
	var out []string
	for _, v := range values {
		if v != nil {
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
}

// resolve follows a $ref to components/schemas, keeping a description set
// next to the $ref.
func resolve(s rawSchema, schemas map[string]rawSchema) rawSchema {
	name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/")
	if !ok {
		return s
	}
	r := schemas[name]
	if s.Description != "" {
		r.Description = s.Description
	}
	return r
}

// properties returns a schema's properties sorted by name, with the fields of
// object (or array of object) properties when nested is set. A property without
// a description takes the one from the schema it refers to.
func properties(s rawSchema, schemas map[string]rawSchema, nested bool) []Param {
	var params []Param
	for name, prop := range s.Properties {
		prop = resolve(prop, schemas)
		item := prop
		if prop.Type == "array" && prop.Items != nil {
			item = resolve(*prop.Items, schemas)
		}
		desc := prop.Description
		if desc == "" {
			desc = item.Description
		}
		p := Param{Name: name, Description: desc, Required: slices.Contains(s.Required, name), Type: string(prop.Type), Enum: enum(item.Enum)}
		if nested {
			p.Fields = properties(item, schemas, false)
		}
		params = append(params, p)
	}
	sort.Slice(params, func(i, j int) bool { return params[i].Name < params[j].Name })
	return params
}

// Load returns the cached spec if present and usable, else the embedded snapshot.
// A cache that cannot be used is skipped and described in warning.
func Load(configDir, cliVersion string) (s *Spec, warning string, err error) {
	if configDir != "" {
		data, readErr := os.ReadFile(filepath.Join(configDir, cacheFile))
		switch {
		case readErr == nil:
			cached, parseErr := Parse(data, cliVersion)
			if parseErr == nil {
				return cached, "", nil
			}
			warning = fmt.Sprintf("ignoring cached spec (%v); using the built-in spec. Run `judo update` to refresh it.", parseErr)
		case !errors.Is(readErr, os.ErrNotExist):
			warning = fmt.Sprintf("ignoring cached spec (%v); using the built-in spec.", readErr)
		}
	}
	s, err = Parse(Snapshot, cliVersion)
	return s, warning, err
}

// Update downloads the spec from baseURL/openapi.json and caches it. The
// download is validated first, so a bad spec never replaces a good cache.
func Update(ctx context.Context, httpClient *http.Client, baseURL, configDir, cliVersion string) (*Spec, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/openapi.json", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("downloading spec: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, "", err
	}

	s, err := Parse(data, cliVersion)
	if err != nil {
		return nil, "", err
	}
	if len(s.Operations) == 0 {
		return nil, "", errors.New("downloaded spec has no operations with an operationId")
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(configDir, cacheFile)
	tmp, err := os.CreateTemp(configDir, cacheFile+".*")
	if err != nil {
		return nil, "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, "", err
	}
	if err := tmp.Close(); err != nil {
		return nil, "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return nil, "", err
	}
	return s, path, nil
}

func checkVersion(min, cli string) error {
	if min == "" || cli == "dev" {
		return nil
	}
	need, err := parseVersion(min)
	if err != nil {
		return fmt.Errorf("invalid x-min-cli-version %q", min)
	}
	have, err := parseVersion(cli)
	if err != nil {
		return fmt.Errorf("invalid CLI version %q", cli)
	}
	for i := range need {
		if have[i] != need[i] {
			if have[i] < need[i] {
				return fmt.Errorf("this spec requires judoscale CLI %s or newer (you have %s); upgrade the CLI", min, cli)
			}
			return nil
		}
	}
	return nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, errors.New("bad version")
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, errors.New("bad version")
		}
		out[i] = n
	}
	return out, nil
}
