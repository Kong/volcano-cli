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

The agent name is also part of the CLI's usage telemetry. The headers below are
not sent before the first-run telemetry notice, and turning telemetry off stops
them. See [telemetry.md](telemetry.md).

## How the agent is detected

Most coding agents set marker environment variables in the shell they run
commands in. The CLI checks these steps in order and uses the first that names
an agent:

1. `AI_AGENT`, then `AGENT`. These are generic variables an agent can set to
   its own name, such as `AI_AGENT=claude-code`. A value of `1`, `true`, `yes`,
   or `on` means "an agent is running" without naming it, so the CLI keeps
   looking for a name. A value of `0`, `false`, `no`, or `off` is ignored.
2. A variable set by one specific agent. If several are set, the first row in
   this table wins:

   | Agent | Variables |
   |---|---|
   | Claude Cowork | `CLAUDE_CODE_IS_COWORK` |
   | Claude Code | `CLAUDECODE`, `CLAUDE_CODE_ENTRYPOINT` |
   | Codex | `CODEX_THREAD_ID`, `CODEX_SANDBOX`, `CODEX_CI` |
   | Gemini CLI | `GEMINI_CLI` |
   | Cursor CLI | `CURSOR_AGENT` |
   | Cursor | `CURSOR_EXTENSION_HOST_ROLE=agent-exec` |
   | OpenCode | `OPENCODE_CLIENT` |
   | Augment | `AUGMENT_AGENT` |
   | Antigravity | `ANTIGRAVITY_AGENT` |

3. `VOLCANO_AGENT`, for agents that set none of the variables above.
4. `AI_AGENT` or `AGENT` set to `1`, `true`, `yes`, or `on`, with no name found
   in the steps above. The CLI reports the agent as `unknown`.
5. A weak signal: a variable that shows the terminal or workspace the command
   ran in, `CURSOR_TRACE_ID` (Cursor) or `REPL_ID` (Replit), or a GitHub Copilot
   CLI setting people can export in their own shell profile, `COPILOT_MODEL` or
   `COPILOT_ALLOW_ALL` (GitHub Copilot). A person typing in that terminal, or
   with those settings, matches too.

If nothing matches, the CLI reports nothing.

## Set VOLCANO_AGENT

The Volcano skills tell agents to set `VOLCANO_AGENT` on each command. You can
also set it yourself, for example in a custom agent or script:

```sh
VOLCANO_AGENT=my-agent volcano functions deploy --all
```

`VOLCANO_AGENT` is ignored when step 1 or step 2 finds the agent.

## What is sent

Each API request carries these headers when an agent is detected:

| Header | Example | Meaning |
|---|---|---|
| `X-Volcano-Agent` | `claude-code` | The agent name |
| `X-Volcano-Agent-Source` | `ai_agent` | Which step found it: `ai_agent` or `agent` (steps 1 and 4), `marker` (step 2), `volcano_agent` (step 3), or `host` (step 5) |
| `X-Volcano-Agent-Raw` | `claude-code_2-1-299_agent` | The original value of `AI_AGENT`, `AGENT`, or `VOLCANO_AGENT`, when it differs from the name |
| `X-Volcano-Agent-Markers` | `claude-code,cursor` | Every agent whose step 2 or step 5 variables are set. More than one means one agent ran inside another, for example Claude Code run from a Cursor terminal |

The name and raw values are limited to letters, digits, and `._-/@+:`, and to
64 characters. The markers header is a comma-separated list of names from the
table above, so it is not free text.

To see the headers the CLI sends, run any command with `--debug`.
