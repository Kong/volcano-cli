package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/docs"
	"github.com/Kong/volcano-cli/internal/mcp"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/version"
)

const (
	defaultSearchLimit = 10
	maxSearchLimit     = 50
)

// newMCP returns the `docs mcp` command: a long-lived MCP server over stdio.
// Unlike the one-shot subcommands it keeps the parsed corpus and BM25 index
// resident, so an agent doing repeated search→read loops reuses a warm index
// instead of rebuilding it per call.
func newMCP(deps cliruntime.Deps, f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run a Model Context Protocol server exposing docs search over stdio",
		Long: "mcp runs a long-lived MCP (Model Context Protocol) server on stdio, " +
			"exposing docs_search, docs_get, and docs_list as tools for an AI agent. " +
			"The docs index is built once and reused across calls.\n\n" +
			"stdout carries only JSON-RPC frames; diagnostics go to stderr.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := f.service(deps)
			if err != nil {
				return err
			}
			handler := &docsHandler{svc: svc, offline: f.offline}
			info := mcp.ServerInfo{Name: "volcano-docs", Version: version.Version}
			fmt.Fprintln(cmd.ErrOrStderr(), "volcano docs mcp: ready on stdio")
			return mcp.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), info, handler)
		},
	}
}

// docsHandler adapts docs.Service to the MCP tool surface.
type docsHandler struct {
	svc     *docs.Service
	offline bool
}

func (h *docsHandler) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "docs_search",
			Description: "Search the Volcano documentation and return ranked sections with snippets. Use this to find docs relevant to a task or to validate assumptions.",
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","description":"Search query","minLength":1},"topic":{"type":"string","description":"Restrict to a top-level docs topic (e.g. authentication, storage)"},"limit":{"type":"integer","description":"Max results (1-50, default 10)","minimum":1,"maximum":50}}}`),
		},
		{
			Name:        "docs_get",
			Description: "Read a document by path, or a single section when the id includes a #anchor (as returned by docs_search).",
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["id"],"properties":{"id":{"type":"string","description":"Document path or path#anchor","minLength":1}}}`),
		},
		{
			Name:        "docs_list",
			Description: "List available documents, optionally filtered by topic.",
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"topic":{"type":"string","description":"Restrict to a top-level docs topic"}}}`),
		},
	}
}

func (h *docsHandler) Call(ctx context.Context, name string, args json.RawMessage) (mcp.ToolResult, error) {
	switch name {
	case "docs_search":
		return h.search(ctx, args)
	case "docs_get":
		return h.get(ctx, args)
	case "docs_list":
		return h.list(ctx, args)
	default:
		return mcp.ToolResult{}, &mcp.Error{Code: mcp.CodeInvalidParams, Message: fmt.Sprintf("unknown tool %q", name)}
	}
}

func (h *docsHandler) search(ctx context.Context, args json.RawMessage) (mcp.ToolResult, error) {
	var in struct {
		Query string `json:"query"`
		Topic string `json:"topic"`
		Limit int    `json:"limit"`
	}
	if err := unmarshalArgs(args, &in); err != nil {
		return mcp.ToolResult{}, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return mcp.ToolResult{}, &mcp.Error{Code: mcp.CodeInvalidParams, Message: "query is required"}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	results, err := h.svc.Search(ctx, in.Query, in.Topic, limit, h.offline)
	if err != nil {
		return errorResult("search", h.svc, h.offline, err)
	}
	return okResult("search", h.svc, h.offline, results)
}

func (h *docsHandler) get(ctx context.Context, args json.RawMessage) (mcp.ToolResult, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := unmarshalArgs(args, &in); err != nil {
		return mcp.ToolResult{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return mcp.ToolResult{}, &mcp.Error{Code: mcp.CodeInvalidParams, Message: "id is required"}
	}
	res, err := h.svc.Get(ctx, in.ID, h.offline)
	if err != nil {
		return errorResult("get", h.svc, h.offline, err)
	}
	return okResult("get", h.svc, h.offline, res)
}

func (h *docsHandler) list(ctx context.Context, args json.RawMessage) (mcp.ToolResult, error) {
	var in struct {
		Topic string `json:"topic"`
	}
	if err := unmarshalArgs(args, &in); err != nil {
		return mcp.ToolResult{}, err
	}
	items, err := h.svc.List(ctx, in.Topic, h.offline)
	if err != nil {
		return errorResult("list", h.svc, h.offline, err)
	}
	return okResult("list", h.svc, h.offline, items)
}

// unmarshalArgs decodes tool arguments, treating malformed input as a protocol
// (invalid params) error rather than a domain failure.
func unmarshalArgs(args json.RawMessage, v any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, v); err != nil {
		return &mcp.Error{Code: mcp.CodeInvalidParams, Message: "invalid tool arguments: " + err.Error()}
	}
	return nil
}

// okResult / errorResult wrap the shared JSON envelope as an MCP tool result so
// the machine contract is identical to the CLI `--json` output.
func okResult(command string, svc *docs.Service, offline bool, data any) (mcp.ToolResult, error) {
	return mcp.TextResult(buildEnvelope(command, svc, offline, "", data), false)
}

func errorResult(command string, svc *docs.Service, offline bool, err error) (mcp.ToolResult, error) {
	return mcp.TextResult(buildErrorEnvelope(command, svc, offline, err), true)
}
