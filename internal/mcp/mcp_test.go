package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHandler is a minimal Handler for protocol tests.
type fakeHandler struct {
	calls int
}

func (h *fakeHandler) Tools() []Tool {
	return []Tool{
		{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
}

func (h *fakeHandler) Call(_ context.Context, name string, args json.RawMessage) (ToolResult, error) {
	h.calls++
	switch name {
	case "echo":
		return TextResult(map[string]any{"args": args}, false)
	case "boom":
		return TextResult(map[string]any{"error": "domain failure"}, true)
	default:
		return ToolResult{}, &Error{Code: CodeInvalidParams, Message: "unknown tool"}
	}
}

// run drives Serve with the given newline-delimited requests and returns the
// decoded response objects (one per output line).
func run(t *testing.T, h Handler, requests ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out bytes.Buffer
	err := Serve(context.Background(), in, &out, ServerInfo{Name: "test", Version: "1"}, h)
	require.NoError(t, err)

	var resps []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var o map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &o), "each stdout line must be valid JSON-RPC: %q", line)
		assert.Equal(t, "2.0", o["jsonrpc"])
		resps = append(resps, o)
	}
	return resps
}

const initReq = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`

func TestInitializeHandshake(t *testing.T) {
	resps := run(t, &fakeHandler{}, initReq)
	require.Len(t, resps, 1)
	result := resps[0]["result"].(map[string]any)
	assert.Equal(t, ProtocolVersion, result["protocolVersion"])
	caps := result["capabilities"].(map[string]any)
	_, hasTools := caps["tools"]
	assert.True(t, hasTools, "must declare tools capability")
	assert.Equal(t, "test", result["serverInfo"].(map[string]any)["name"])
}

func TestNotificationProducesNoResponse(t *testing.T) {
	resps := run(t, &fakeHandler{},
		initReq,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	)
	// Only the initialize response; the notification yields nothing.
	require.Len(t, resps, 1)
	assert.EqualValues(t, 1, resps[0]["id"])
}

func TestToolsListAndCall(t *testing.T) {
	resps := run(t, &fakeHandler{},
		initReq,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}`,
	)
	require.Len(t, resps, 3)
	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	require.Len(t, tools, 1)
	assert.Equal(t, "echo", tools[0].(map[string]any)["name"])

	call := resps[2]["result"].(map[string]any)
	assert.NotEqual(t, true, call["isError"])
	content := call["content"].([]any)
	assert.Equal(t, "text", content[0].(map[string]any)["type"])
}

func TestDomainErrorIsToolResultNotRPCError(t *testing.T) {
	resps := run(t, &fakeHandler{},
		initReq,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"boom","arguments":{}}}`,
	)
	call := resps[1]["result"].(map[string]any)
	assert.Equal(t, true, call["isError"])
	assert.Nil(t, resps[1]["error"], "domain failure must not be a JSON-RPC error")
}

func TestUnknownToolIsRPCError(t *testing.T) {
	resps := run(t, &fakeHandler{},
		initReq,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
	)
	errObj := resps[1]["error"].(map[string]any)
	assert.EqualValues(t, CodeInvalidParams, errObj["code"])
}

func TestUnknownMethodIsMethodNotFound(t *testing.T) {
	resps := run(t, &fakeHandler{},
		initReq,
		`{"jsonrpc":"2.0","id":7,"method":"does/not/exist"}`,
	)
	errObj := resps[1]["error"].(map[string]any)
	assert.EqualValues(t, CodeMethodNotFound, errObj["code"])
	assert.EqualValues(t, 7, resps[1]["id"])
}

func TestToolsListBeforeInitializeRejected(t *testing.T) {
	resps := run(t, &fakeHandler{},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	)
	errObj := resps[0]["error"].(map[string]any)
	assert.EqualValues(t, CodeInvalidRequest, errObj["code"])
}

func TestMalformedLineIsParseError(t *testing.T) {
	resps := run(t, &fakeHandler{}, `{not json`)
	errObj := resps[0]["error"].(map[string]any)
	assert.EqualValues(t, CodeParseError, errObj["code"])
	assert.Nil(t, resps[0]["id"])
}

func TestStringAndNumericIDsPreserved(t *testing.T) {
	resps := run(t, &fakeHandler{},
		initReq,
		`{"jsonrpc":"2.0","id":"abc","method":"ping"}`,
	)
	assert.EqualValues(t, "abc", resps[1]["id"])
}

func TestDuplicateInitializeRejected(t *testing.T) {
	resps := run(t, &fakeHandler{}, initReq, initReq)
	require.Len(t, resps, 2)
	errObj := resps[1]["error"].(map[string]any)
	assert.EqualValues(t, CodeInvalidRequest, errObj["code"])
}

func TestNotificationForKnownMethodProducesNoResponse(t *testing.T) {
	// ping/tools-list sent without an id are notifications — no reply allowed.
	resps := run(t, &fakeHandler{},
		initReq,
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`,
	)
	// Only the initialize response; the two id-less calls yield nothing.
	require.Len(t, resps, 1)
	assert.EqualValues(t, 1, resps[0]["id"])
}
