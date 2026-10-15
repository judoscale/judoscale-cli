package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func testServer() *Server {
	return &Server{
		Name:         "test",
		Version:      "1.2.3",
		Instructions: "be careful",
		Tools: []Tool{{
			Name:        "echo",
			InputSchema: map[string]any{"type": "object"},
			Annotations: &Annotations{ReadOnlyHint: true},
		}},
		Call: func(ctx context.Context, name string, args map[string]any) (string, error) {
			if args["fail"] != nil {
				return "", errors.New("boom")
			}
			b, _ := json.Marshal(args)
			return string(b), nil
		},
	}
}

// exchange sends the messages and returns the responses, one per line.
func exchange(t *testing.T, s *Server, messages ...string) []map[string]any {
	t.Helper()
	var out strings.Builder
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(messages, "\n")), &out); err != nil {
		t.Fatal(err)
	}
	var resps []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad response %q: %v", line, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func TestInitialize(t *testing.T) {
	resps := exchange(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("a notification got a response: %v", resps)
	}
	result := resps[0]["result"].(map[string]any)
	if result["protocolVersion"] != "2025-03-26" || result["instructions"] != "be careful" {
		t.Errorf("initialize = %v", result)
	}
	if info := result["serverInfo"].(map[string]any); info["name"] != "test" || info["version"] != "1.2.3" {
		t.Errorf("serverInfo = %v", info)
	}
	if _, ok := result["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("capabilities = %v", result["capabilities"])
	}
	if resps[1]["id"].(float64) != 2 || resps[1]["result"] == nil {
		t.Errorf("ping = %v", resps[1])
	}
}

func TestInitializeFallsBackToNewestVersion(t *testing.T) {
	resps := exchange(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if got := resps[0]["result"].(map[string]any)["protocolVersion"]; got != protocolVersions[0] {
		t.Errorf("version = %v", got)
	}
}

func TestToolsList(t *testing.T) {
	resps := exchange(t, testServer(), `{"jsonrpc":"2.0","id":"a","method":"tools/list"}`)
	tools := resps[0]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "echo" {
		t.Errorf("tools = %v", tools)
	}
	if resps[0]["id"] != "a" {
		t.Errorf("id = %v", resps[0]["id"])
	}
	if ann := tools[0].(map[string]any)["annotations"].(map[string]any); ann["readOnlyHint"] != true {
		t.Errorf("annotations = %v", ann)
	}
}

func TestToolsCall(t *testing.T) {
	resps := exchange(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"id":12345678901234567890}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"fail":true}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nope"}}`,
	)
	text := func(i int) (string, bool) {
		r := resps[i]["result"].(map[string]any)
		return r["content"].([]any)[0].(map[string]any)["text"].(string), r["isError"].(bool)
	}
	if got, isErr := text(0); got != `{"id":12345678901234567890}` || isErr {
		t.Errorf("call = %q error=%v", got, isErr)
	}
	if got, isErr := text(1); got != "boom" || !isErr {
		t.Errorf("failed call = %q error=%v", got, isErr)
	}
	if resps[2]["error"] == nil {
		t.Errorf("unknown tool: %v", resps[2])
	}
}

func TestUnknownMethod(t *testing.T) {
	resps := exchange(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","method":"something/else"}`,
	)
	if len(resps) != 1 || resps[0]["error"].(map[string]any)["code"].(float64) != codeMethodNotFound {
		t.Errorf("%v", resps)
	}
}

func TestBadInputDoesNotStopTheServer(t *testing.T) {
	resps := exchange(t, testServer(),
		`this is not json`,
		`[1,2]`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
	)
	if len(resps) != 3 {
		t.Fatalf("%d responses: %v", len(resps), resps)
	}
	if code := resps[0]["error"].(map[string]any)["code"].(float64); code != codeParse || resps[0]["id"] != nil {
		t.Errorf("parse error = %v", resps[0])
	}
	if code := resps[1]["error"].(map[string]any)["code"].(float64); code != codeInvalidRequest {
		t.Errorf("invalid request = %v", resps[1])
	}
	if resps[2]["result"] == nil {
		t.Errorf("ping after bad input = %v", resps[2])
	}
}
