---
title: "Agent detection"
description: "How the Volcano CLI detects which coding agent ran a command, what it reports to the Volcano API, and how to set VOLCANO_AGENT."
---

## What it is

When a coding agent runs a `volcano` command, the CLI works out which agent it
was and reports the name to the Volcano API with each request. This tells the
Volcano team which agents people build with. It never changes what a command
does, and it is never used to allow or block a request.

The CLI reads environment variables only. It does not read files, inspect other
processes, or send command arguments.

## How the agent is detected

Most coding agents set marker environment variables in the shell they run
commands in. The CLI checks them in this order and uses the first match:

1. `AI_AGENT`, then `AGENT`. These are generic variables an agent can set to
   its own name, such as `AI_AGENT=claude-code`. A value of `1` or `true` means
   "an agent is running" without naming it, so the CLI keeps looking for a
   name. A value of `0`, `false`, `no`, or `off` is ignored.
2. A variable set by one specific agent:

   | Agent | Variables |
   |---|---|
   | Claude Code | `CLAUDECODE`, `CLAUDE_CODE_ENTRYPOINT` |
   | Claude Cowork | `CLAUDE_CODE_IS_COWORK` |
   | Codex | `CODEX_THREAD_ID`, `CODEX_SANDBOX`, `CODEX_CI` |
   | Gemini CLI | `GEMINI_CLI` |
   | Cursor | `CURSOR_EXTENSION_HOST_ROLE=agent-exec`, `CURSOR_TRACE_ID` |
   | Cursor CLI | `CURSOR_AGENT` |
   | OpenCode | `OPENCODE_CLIENT` |
   | Augment | `AUGMENT_AGENT` |
   | Antigravity | `ANTIGRAVITY_AGENT` |
   | GitHub Copilot | `COPILOT_MODEL`, `COPILOT_ALLOW_ALL` |
   | Replit | `REPL_ID` |

3. `VOLCANO_AGENT`, for agents that set none of the variables above.

If nothing matches, the CLI reports nothing.

## Set VOLCANO_AGENT

The Volcano skills tell agents to set `VOLCANO_AGENT` on each command. You can
also set it yourself, for example in a custom agent or script:

```sh
VOLCANO_AGENT=my-agent volcano functions deploy --all
```

`VOLCANO_AGENT` is used only when none of the variables in the earlier steps
are set.

## What is sent

Each API request carries these headers when an agent is detected:

| Header | Example | Meaning |
|---|---|---|
| `X-Volcano-Agent` | `claude-code` | The agent name |
| `X-Volcano-Agent-Source` | `ai_agent` | Which step found it: `ai_agent`, `agent`, `marker`, or `volcano_agent` |
| `X-Volcano-Agent-Raw` | `claude-code_2-1-299_agent` | The original value of `AI_AGENT`, `AGENT`, or `VOLCANO_AGENT`, when it differs from the name |
| `X-Volcano-Agent-Markers` | `claude-code,cursor` | Every agent whose variables are set. More than one means one agent started another, for example Claude Code run from a Cursor terminal |

Values are limited to letters, digits, and `._-/@+:`, and to 64 characters.

To see the headers the CLI sends, run any command with `--debug`.
