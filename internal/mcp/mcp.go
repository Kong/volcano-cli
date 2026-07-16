// Package mcp implements a minimal, dependency-free Model Context Protocol
// server over stdio (newline-delimited JSON-RPC 2.0). It supports exactly the
// lifecycle and methods the `volcano docs` tool surface needs — initialize,
// notifications/initialized, ping, tools/list, tools/call — and nothing more.
//
// The protocol loop is exposed as Serve(ctx, reader, writer, info, handler) so
// it can be driven directly with in-memory streams in tests, without spawning
// a subprocess. Domain logic lives entirely behind the Handler interface.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the MCP spec revision this server advertises.
const ProtocolVersion = "2025-06-18"

// maxFrame bounds a single inbound JSON-RPC line to avoid unbounded memory use.
const maxFrame = 8 << 20 // 8 MiB

// JSON-RPC 2.0 standard error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// ServerInfo identifies this server to the client during initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool describes one callable tool advertised via tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Content is a single content item in a tool result (text only here).
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult is the payload returned from a successful tools/call. IsError
// signals a domain-level failure (the call completed but the operation failed),
// as distinct from a protocol error.
type ToolResult struct {
	Content           []Content `json:"content"`
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

// TextResult builds a ToolResult carrying one JSON text content item plus the
// same value as structuredContent.
func TextResult(v any, isError bool) (ToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{
		Content:           []Content{{Type: "text", Text: string(data)}},
		StructuredContent: v,
		IsError:           isError,
	}, nil
}

// Error is a protocol-level error a Handler may return to produce a JSON-RPC
// error response (e.g. unknown tool, invalid arguments). Domain failures should
// instead be returned as a ToolResult with IsError=true.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Handler supplies the tool surface backing the server.
type Handler interface {
	Tools() []Tool
	Call(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
}

// wire types.
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

type initializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    caps       `json:"capabilities"`
	ServerInfo      ServerInfo `json:"serverInfo"`
}

type caps struct {
	Tools map[string]any `json:"tools"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

// Serve runs the JSON-RPC 2.0 stdio loop until EOF, a fatal read error, or
// context cancellation. It writes only protocol frames to w; callers must keep
// all diagnostics on a separate stream (stderr).
func Serve(ctx context.Context, r io.Reader, w io.Writer, info ServerInfo, h Handler) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxFrame)

	initialized := false

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeError(w, nil, CodeParseError, "parse error")
			continue
		}
		isNotification := len(req.ID) == 0

		// A JSON-RPC notification (no id) must never receive a response. Only
		// notifications/* are meaningful as notifications; ignore any other
		// method sent without an id rather than replying with a forbidden
		// id:null frame.
		if isNotification && req.Method != "notifications/initialized" {
			continue
		}

		switch req.Method {
		case "initialize":
			if initialized {
				writeError(w, req.ID, CodeInvalidRequest, "already initialized")
				continue
			}
			initialized = true
			writeResult(w, req.ID, initializeResult{
				ProtocolVersion: ProtocolVersion,
				Capabilities:    caps{Tools: map[string]any{}},
				ServerInfo:      info,
			})
		case "notifications/initialized":
			// Client confirmation; no response.
		case "ping":
			writeResult(w, req.ID, map[string]any{})
		case "tools/list":
			if !initialized {
				writeError(w, req.ID, CodeInvalidRequest, "server not initialized")
				continue
			}
			writeResult(w, req.ID, toolsListResult{Tools: h.Tools()})
		case "tools/call":
			if !initialized {
				writeError(w, req.ID, CodeInvalidRequest, "server not initialized")
				continue
			}
			handleToolCall(ctx, w, req, h)
		default:
			writeError(w, req.ID, CodeMethodNotFound, fmt.Sprintf("unknown method %q", req.Method))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("mcp read error: %w", err)
	}
	return nil
}

func handleToolCall(ctx context.Context, w io.Writer, req rpcRequest, h Handler) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeError(w, req.ID, CodeInvalidParams, "invalid tools/call params")
		return
	}
	if params.Name == "" {
		writeError(w, req.ID, CodeInvalidParams, "missing tool name")
		return
	}
	res, err := h.Call(ctx, params.Name, params.Arguments)
	if err != nil {
		perr := &Error{Code: CodeInternalError, Message: err.Error()}
		var e *Error
		if errors.As(err, &e) {
			perr = e
		}
		writeError(w, req.ID, perr.Code, perr.Message)
		return
	}
	writeResult(w, req.ID, res)
}

func writeResult(w io.Writer, id json.RawMessage, result any) {
	writeResponse(w, rpcResponse{JSONRPC: "2.0", ID: normalizeID(id), Result: result})
}

func writeError(w io.Writer, id json.RawMessage, code int, msg string) {
	writeResponse(w, rpcResponse{JSONRPC: "2.0", ID: normalizeID(id), Error: &rpcError{Code: code, Message: msg}})
}

func writeResponse(w io.Writer, resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		data, _ = json.Marshal(rpcResponse{JSONRPC: "2.0", ID: resp.ID, Error: &rpcError{Code: CodeInternalError, Message: "failed to encode response"}})
	}
	data = append(data, '\n')
	_, _ = w.Write(data)
}

// normalizeID returns a JSON null id for responses to messages that lacked one
// (JSON-RPC requires an id member on responses).
func normalizeID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}
