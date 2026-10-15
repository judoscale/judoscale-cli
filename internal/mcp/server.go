// Package mcp is a minimal Model Context Protocol server for tools, speaking
// JSON-RPC 2.0 over a stream with one message per line (the stdio transport).
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
)

// protocolVersions are the versions this server speaks, newest first.
var protocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations *Annotations   `json:"annotations,omitempty"`
}

// Annotations are hints that let clients decide when to ask for approval.
type Annotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

type Server struct {
	Name, Version string
	// Instructions are passed to the model when the client connects.
	Instructions string
	Tools        []Tool
	// Call runs a tool. An error is reported to the model as a failed call,
	// so it can see what went wrong.
	Call func(ctx context.Context, name string, args map[string]any) (string, error)
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// Serve handles requests from r until it is closed, writing responses to w.
// Messages are one per line, so a line that can't be parsed gets an error
// response and the server carries on.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	enc := json.NewEncoder(w)
	for {
		line, readErr := br.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			if resp := s.handleLine(ctx, line); resp != nil {
				if err := enc.Encode(resp); err != nil {
					return err
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func (s *Server) handleLine(ctx context.Context, line []byte) *response {
	if !json.Valid(line) {
		return &response{JSONRPC: "2.0", Error: &rpcError{codeParse, "parse error"}}
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return &response{JSONRPC: "2.0", Error: &rpcError{codeInvalidRequest, "invalid request"}}
	}
	return s.handle(ctx, req)
}

// handle returns nil for notifications, which get no response.
func (s *Server) handle(ctx context.Context, req request) *response {
	isNotification := len(req.ID) == 0
	result, rpcErr := s.dispatch(ctx, req)
	if isNotification {
		return nil
	}
	resp := &response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	if rpcErr == nil {
		b, err := json.Marshal(result)
		if err != nil {
			resp.Error = &rpcError{codeInternal, err.Error()}
		} else {
			resp.Result = b
		}
	}
	return resp
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		version := protocolVersions[0]
		if slices.Contains(protocolVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		result := map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			result["instructions"] = s.Instructions
		}
		return result, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools := s.Tools
		if tools == nil {
			tools = []Tool{}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		return s.callTool(ctx, req.Params)
	}
	return nil, &rpcError{codeMethodNotFound, "method not found: " + req.Method}
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	dec := json.NewDecoder(bytes.NewReader(params))
	dec.UseNumber() // keep integers as written
	if err := dec.Decode(&p); err != nil {
		return nil, &rpcError{codeInvalidParams, "invalid tool call: " + err.Error()}
	}
	if !slices.ContainsFunc(s.Tools, func(t Tool) bool { return t.Name == p.Name }) {
		return nil, &rpcError{codeInvalidParams, "unknown tool: " + p.Name}
	}

	text, err := s.Call(ctx, p.Name, p.Arguments)
	isError := err != nil
	if isError {
		text = err.Error()
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}, nil
}
