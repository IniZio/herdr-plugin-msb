package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

const (
	ProtocolVersion = "2025-06-18"
	ServerName      = "herdr-plugin-msb"
	ServerVersion   = "v0.1.0"

	maxLineBytes = 8 << 20
)

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Server speaks newline-delimited JSON-RPC 2.0 (the MCP stdio framing).
type Server struct {
	rt    coreruntime.Runtime
	tools []Tool
}

func NewServer(rt coreruntime.Runtime) *Server {
	return &Server{rt: rt, tools: buildTools(rt)}
}

func (s *Server) Tools() []Tool { return s.tools }

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		resp, reply := s.handleLine(ctx, line)
		if !reply {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("mcp: write response: %w", err)
		}
	}
	return scanner.Err()
}

func (s *Server) handleLine(ctx context.Context, line []byte) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return errResponse(nil, codeParseError, fmt.Sprintf("parse error: %v", err)), true
	}
	isNotification := len(req.ID) == 0
	if req.JSONRPC != "2.0" {
		if isNotification {
			return rpcResponse{}, false
		}
		return errResponse(req.ID, codeInvalidRequest, `jsonrpc must be "2.0"`), true
	}
	result, rerr := s.dispatch(ctx, req)
	if isNotification {
		return rpcResponse{}, false
	}
	if rerr != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}, true
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, true
}

func (s *Server) dispatch(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": ServerName, "version": ServerVersion},
		}, nil
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.descriptors()}, nil
	case "tools/call":
		return s.callTool(ctx, req.Params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
}

func (s *Server) descriptors() []toolDescriptor {
	out := make([]toolDescriptor, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, toolDescriptor{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return out
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params callToolParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("invalid params: %v", err)}
		}
	}
	if params.Name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call requires a tool name"}
	}
	for _, t := range s.tools {
		if t.Name != params.Name {
			continue
		}
		payload, err := t.Handler(ctx, params.Arguments)
		if err != nil {
			return callToolResult{Content: []textContent{{Type: "text", Text: err.Error()}}, IsError: true}, nil
		}
		body, merr := json.Marshal(payload)
		if merr != nil {
			text := fmt.Sprintf("%s: marshal result: %v", t.Name, merr)
			return callToolResult{Content: []textContent{{Type: "text", Text: text}}, IsError: true}, nil
		}
		return callToolResult{Content: []textContent{{Type: "text", Text: string(body)}}}, nil
	}
	return nil, &rpcError{
		Code:    codeInvalidParams,
		Message: fmt.Sprintf("unknown tool %q; call tools/list for the advertised set", params.Name),
	}
}

func errResponse(id json.RawMessage, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
