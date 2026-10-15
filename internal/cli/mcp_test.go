package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/judoscale/judoscale-cli/internal/spec"
)

// mcpRun sends JSON-RPC messages to `judo mcp` and returns the responses.
func mcpRun(t *testing.T, srv *httptest.Server, args []string, messages ...string) []map[string]any {
	t.Helper()
	res := run(t, srv, nil, strings.Join(messages, "\n"), append([]string{"mcp"}, args...)...)
	if res.code != 0 {
		t.Fatalf("%+v", res)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(res.stdout), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad output %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func mcpTools(t *testing.T, args ...string) map[string]map[string]any {
	t.Helper()
	resps := mcpRun(t, nil, args, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := map[string]map[string]any{}
	for _, raw := range resps[0]["result"].(map[string]any)["tools"].([]any) {
		tool := raw.(map[string]any)
		tools[tool["name"].(string)] = tool
	}
	return tools
}

func TestMCPReadOnlyByDefault(t *testing.T) {
	s, err := spec.Parse(spec.Snapshot, "dev")
	if err != nil {
		t.Fatal(err)
	}
	var reads int
	for _, op := range s.Operations {
		if op.Method == http.MethodGet {
			reads++
		}
	}

	tools := mcpTools(t)
	if len(tools) != reads {
		t.Errorf("%d tools, want %d reads", len(tools), reads)
	}
	for _, name := range []string{"list_apps", "get_process_metric_series", "list_process_scale_events"} {
		if tools[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range []string{"update_app", "delete_app", "enable_app_maintenance", "create_api_token"} {
		if tools[name] != nil {
			t.Errorf("%s exposed without --writes", name)
		}
	}

	all := mcpTools(t, "--writes")
	if len(all) != len(s.Operations) {
		t.Errorf("%d tools with --writes, want %d", len(all), len(s.Operations))
	}
}

func TestMCPToolSchema(t *testing.T) {
	tools := mcpTools(t, "--writes")

	update := tools["update_process"]
	schema := update["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	if props["id"].(map[string]any)["type"] != "integer" && props["id"].(map[string]any)["type"] != "string" {
		t.Errorf("id = %v", props["id"])
	}
	if props["max_scale"].(map[string]any)["type"] != "integer" {
		t.Errorf("max_scale = %v", props["max_scale"])
	}
	if req := schema["required"].([]any); len(req) != 1 || req[0] != "id" {
		t.Errorf("required = %v", req)
	}
	if update["annotations"].(map[string]any)["readOnlyHint"] != false {
		t.Errorf("annotations = %v", update["annotations"])
	}

	if ann := tools["delete_app"]["annotations"].(map[string]any); ann["destructiveHint"] != true {
		t.Errorf("apps_delete = %v", ann)
	}
	if ann := tools["list_apps"]["annotations"].(map[string]any); ann["readOnlyHint"] != true {
		t.Errorf("apps_list = %v", ann)
	}

	role := tools["update_membership"]["inputSchema"].(map[string]any)["properties"].(map[string]any)["role"].(map[string]any)
	if len(role["enum"].([]any)) == 0 {
		t.Errorf("role = %v", role)
	}
}

func TestMCPToolCall(t *testing.T) {
	var method, path, body string
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, path, body = r.Method, r.URL.Path, string(b)
		w.Write([]byte("{\n  \"id\": 3,\n  \"name\": \"web\"\n}"))
	})

	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"update_process","arguments":{"id":7,"max_scale":10,"autoscale_metrics":[{"metric":"queue_time"}]}}}`
	resps := mcpRun(t, srv, []string{"--writes"}, call)
	result := resps[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"]
	if result["isError"] != false || text != `{"id":3,"name":"web"}` {
		t.Errorf("result = %v", result)
	}
	if method != "PATCH" || path != "/v1/processes/7" || body != `{"autoscale_metrics":[{"metric":"queue_time"}],"max_scale":10}` {
		t.Errorf("sent %s %s %s", method, path, body)
	}

	get := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_process_metric_series","arguments":{"process_id":5,"metric":"queue_time","from":"A","to":"B"}}}`
	mcpRun(t, srv, nil, get)
	if method != "GET" || path != "/v1/processes/5/metrics/queue_time" {
		t.Errorf("sent %s %s", method, path)
	}
}

func TestMCPToolErrors(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"type":"not_found","message":"No such app"}}`))
	})
	errText := func(resp map[string]any) string {
		result := resp["result"].(map[string]any)
		if result["isError"] != true {
			t.Errorf("not an error: %v", resp)
		}
		return result["content"].([]any)[0].(map[string]any)["text"].(string)
	}

	resps := mcpRun(t, srv, nil,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_app","arguments":{"id":1}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_app","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_app","arguments":{"id":1,"bogus":true}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete_app","arguments":{"id":1}}}`,
	)
	if got := errText(resps[0]); !strings.Contains(got, "not_found: No such app") {
		t.Errorf("API error = %q", got)
	}
	if got := errText(resps[1]); !strings.Contains(got, `missing required argument "id"`) {
		t.Errorf("missing arg = %q", got)
	}
	if got := errText(resps[2]); !strings.Contains(got, `unknown argument "bogus"`) {
		t.Errorf("unknown arg = %q", got)
	}
	// A write tool isn't listed in read-only mode, so it can't be called.
	if resps[3]["error"] == nil {
		t.Errorf("write tool callable without --writes: %v", resps[3])
	}
}

func TestMCPInstructionsMentionReadOnly(t *testing.T) {
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	text := func(args ...string) string {
		return mcpRun(t, nil, args, init)[0]["result"].(map[string]any)["instructions"].(string)
	}
	if got := text(); !strings.Contains(got, "--writes") {
		t.Errorf("read-only instructions = %q", got)
	}
	if got := text("--writes"); strings.Contains(got, "Read-only") {
		t.Errorf("writes instructions = %q", got)
	}
}

func TestMCPUsage(t *testing.T) {
	if res := run(t, nil, nil, "", "mcp", "--nope"); res.code != 2 {
		t.Errorf("%+v", res)
	}
	if res := run(t, nil, nil, "", "mcp", "--help"); res.code != 0 || !strings.Contains(res.stdout, "--writes") {
		t.Errorf("%+v", res)
	}
}
