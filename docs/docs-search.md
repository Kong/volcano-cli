---
title: "Documentation search (`volcano docs`)"
description: "Search and read the official Volcano documentation from your terminal with volcano docs, cached locally for offline use."
---

`volcano docs` fetches the official Volcano documentation into a local cache and
lets you search and read it from your terminal — offline, and without leaving
your workflow. It is designed for both humans and AI agents: `--json` output is
a stable, versioned contract so an agent driving the CLI can look things up and
validate its assumptions against the docs.

## Commands

```bash
# Fetch or refresh the local cache from the source repository
volcano docs sync

# Search the docs (ranked, with snippets)
volcano docs search "reset password email"
volcano docs search "service key" --topic authentication -n 5

# Read a whole document, or a single section by its #anchor
volcano docs get authentication/password-reset.md
volcano docs get "authentication/password-reset.md#how-it-works"

# List the available documents
volcano docs list --topic storage
```

Reads (`search`, `get`, `list`) bootstrap the cache automatically on first use.
After that they never touch the network; run `volcano docs sync` to refresh.
Pass `--offline` to guarantee no network access (a missing cache then errors).

## MCP server mode (for agents)

`volcano docs mcp` runs a long-lived [Model Context Protocol](https://modelcontextprotocol.io)
server over stdio, exposing the docs as agent tools. Unlike the one-shot
subcommands (which rebuild the ~6.7 MB BM25 index on every call), the server
builds the index once and keeps it resident, so an agent doing repeated
search→read loops reuses a warm index.

Tools: `docs_search` (`query`, optional `topic`, `limit`), `docs_get` (`id` =
`path` or `path#anchor`), and `docs_list` (optional `topic`). Each tool returns
the same versioned envelope as `--json`, both as text content and as
`structuredContent`; domain failures come back as a tool result with
`isError: true` and a `DOCS_*` code, while malformed calls use JSON-RPC errors.
stdout carries only JSON-RPC frames; diagnostics go to stderr.

Register it with an MCP client, e.g. Claude Code:

```json
{
  "mcpServers": {
    "volcano-docs": { "command": "volcano", "args": ["docs", "mcp"] }
  }
}
```

The index is rebuilt automatically if an external `volcano docs sync` publishes
a new snapshot while the server is running.

## How search works

The corpus is parsed into heading-delimited sections. Each search builds a small
in-memory BM25 index over those sections with boosts for titles, headings,
paths, exact phrases, and technical tokens (identifiers, flags, CLI commands),
then returns the best-matching sections with a snippet and source line range. No
index is persisted and no external services are used.

## JSON output for agents

Every command supports `--json`, emitting a single versioned envelope on stdout
(progress and warnings go to stderr):

```jsonc
{
  "schema_version": 1,
  "command": "search",
  "source": {
    "provider": "github",
    "repository": "Kong/volcano-cli",
    "ref": "main",
    "path": "docs",
    "resolved_commit": "…"
  },
  "cache": { "key": "…", "synced_at": "…", "checked_at": "…", "stale": false, "offline": false },
  "data": [
    {
      "id": "authentication/password-reset.md#how-it-works",
      "rank": 1,
      "score": 12.34,
      "path": "authentication/password-reset.md",
      "topic": "authentication",
      "title": "Password Reset",
      "heading_path": ["Password Reset", "How It Works"],
      "anchor": "how-it-works",
      "line_start": 20,
      "line_end": 31,
      "snippet": "…",
      "matched_terms": ["reset", "password"]
    }
  ]
}
```

A result `id` (`path#anchor`) can be passed straight to `volcano docs get` to
read that section. Errors use the same envelope with an `error` object
(`code`, `message`) and a nonzero exit status. Codes include
`DOCS_CACHE_MISSING`, `DOCS_SOURCE_UNAVAILABLE`, `DOCS_INVALID_SOURCE`,
`DOCS_NOT_FOUND`, `DOCS_INVALID_ID`, and `DOCS_SYNC_INCOMPLETE`.

## Configuring the source

The documentation source defaults to the `docs/` directory of the public
`Kong/volcano-cli` repository on `main` — the CLI's own documentation, which
stays versioned with the CLI and needs no authentication. Override it per
invocation or via the environment; precedence is flags → environment → config →
defaults:

| Field      | Flag     | Environment          |
|------------|----------|----------------------|
| repository | `--repo` | `VOLCANO_DOCS_REPO`  |
| git ref    | `--ref`  | `VOLCANO_DOCS_REF`   |
| subdir     | `--path` | `VOLCANO_DOCS_PATH`  |

To search the broader product/API documentation instead, point it at the
hosting repo:

```bash
volcano docs --repo Kong/volcano-hosting --path docs search "row level security"
```

Each source is cached separately. Note that `volcano <command> --help` is the
authoritative, always-current reference for command syntax and flags; the
`docs` corpus complements it with conceptual guides and product context. For
private source repositories, set `GITHUB_TOKEN`; it is sent only to
`api.github.com` and never to any other host.
