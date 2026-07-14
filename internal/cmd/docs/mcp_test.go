package docs

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runMCP drives the `docs mcp` command with newline-delimited JSON-RPC input
// and returns the decoded response objects. This exercises the full path:
// MCP loop → docsHandler → real docs.Service → httptest GitHub backend.
func runMCP(t *testing.T, requests ...string) []map[string]any {
	t.Helper()
	srv := fakeDocsServer(t, map[string]string{
		"authentication/keys.md": "# Keys\n\n## Service keys\nThe service_role key bypasses RLS.",
		"storage/buckets.md":     "# Buckets\n\n## Create\nCreate a storage bucket.",
	})
	deps := testDeps(t, srv)

	cmd := New(deps)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(strings.Join(requests, "\n") + "\n"))
	cmd.SetArgs([]string{"mcp"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	require.NoError(t, cmd.Execute())

	var resps []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var o map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &o), "stdout must be JSON-RPC only: %q", line)
		resps = append(resps, o)
	}
	return resps
}

const mcpInit = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`

func TestMCPToolsListExposesDocsTools(t *testing.T) {
	resps := runMCP(t, mcpInit, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	require.Len(t, resps, 2)
	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, tl := range tools {
		names = append(names, tl.(map[string]any)["name"].(string))
	}
	assert.ElementsMatch(t, []string{"docs_search", "docs_get", "docs_list"}, names)
}

func TestMCPSearchReturnsEnvelope(t *testing.T) {
	resps := runMCP(t, mcpInit,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"docs_search","arguments":{"query":"service key","limit":3}}}`,
	)
	result := resps[1]["result"].(map[string]any)
	assert.NotEqual(t, true, result["isError"])
	sc := result["structuredContent"].(map[string]any)
	assert.EqualValues(t, 1, sc["schema_version"])
	assert.Equal(t, "search", sc["command"])
	data := sc["data"].([]any)
	require.NotEmpty(t, data)
	assert.Contains(t, data[0].(map[string]any)["path"], "authentication/keys.md")
}

func TestMCPGetSection(t *testing.T) {
	resps := runMCP(t, mcpInit,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"docs_get","arguments":{"id":"authentication/keys.md#service-keys"}}}`,
	)
	sc := resps[1]["result"].(map[string]any)["structuredContent"].(map[string]any)
	data := sc["data"].(map[string]any)
	assert.Equal(t, "service-keys", data["anchor"])
	assert.Contains(t, data["content"], "service_role")
}

func TestMCPMissingQueryIsInvalidParams(t *testing.T) {
	resps := runMCP(t, mcpInit,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"docs_search","arguments":{"query":"  "}}}`,
	)
	errObj := resps[1]["error"].(map[string]any)
	assert.EqualValues(t, -32602, errObj["code"])
}

func TestMCPGetNotFoundIsDomainError(t *testing.T) {
	resps := runMCP(t, mcpInit,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"docs_get","arguments":{"id":"nope/missing.md"}}}`,
	)
	result := resps[1]["result"].(map[string]any)
	assert.Equal(t, true, result["isError"])
	sc := result["structuredContent"].(map[string]any)
	errEnv := sc["error"].(map[string]any)
	assert.Equal(t, "DOCS_NOT_FOUND", errEnv["code"])
}
