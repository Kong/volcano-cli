---
title: "Agent setup"
description: "Install the Volcano agent skills and plugins into the coding agents on your machine, and keep them up to date."
---

## What it is

`volcano setup` installs the Volcano skills/plugins into the coding agents
("harnesses") on your machine so they know how to build on Volcano. It detects
what is installed, sets each one up, and reports what changed.

## Harnesses

| Harness | How Volcano is installed |
|---|---|
| `claude-code`, `codex` | via the agent's own plugin marketplace (`Kong/volcano-agentic-plugins`) |
| `cursor`, `opencode`, `pi` | Volcano skills written into the agent's skills directory |
| `manual` (fallback) | skills + `AGENTS.md` under `~/.volcano` when no harness is detected |

## Usage

```bash
volcano setup                        # detect and set up all detected harnesses
volcano setup --agent claude-code    # only the named agent(s)
volcano setup --manual               # force the ~/.volcano manual install
volcano setup --dry-run              # show what would change, write nothing
volcano setup --yes                  # skip the prompt (use in agents/CI)
```

## Run by a coding agent

When a coding agent runs `volcano setup`, it installs for that agent only, not
for every agent on the machine, and prints which one it picked. Pass `--agent`
to choose others.

| Agent | Detected from |
|---|---|
| Claude Code | `CLAUDECODE=1` |
| Codex | `CODEX_THREAD_ID` |
| opencode | `OPENCODE=1` |
| pi | `PI_CODING_AGENT=true` |

Cursor is not detected this way. When more than one of these is set, for
example an agent started from inside another, `setup` behaves as if no agent
ran it.

On a real terminal, `setup` shows a picker of detected harnesses (an installed
harness that is behind its latest version is marked `[outdated]`). If it finds
no coding agents, it shows a message with supported examples instead of a picker,
then installs the manual fallback under `~/.volcano`. It runs
non-interactively when stdin/stdout is piped, in CI, when
`VOLCANO_NONINTERACTIVE` is set, or with `--yes`. Then it installs for the
calling agent if there is one, and for all detected harnesses otherwise.

The picker wraps its heading and key hints in narrow terminals, including split tmux panes. Use the arrow keys to move, Space to toggle an agent, and Enter to confirm.

## Reruns keep things current

`volcano setup` is meant to be re-run. Each run refreshes the source and lands
on the latest version, and the report says what happened per harness:

- `[installed]` — set up for the first time.
- `[updated]` — a newer version replaced an older one; marketplace plugins show
  the version delta (e.g. `0.2.14 → 0.2.16`) and skills show how many files
  changed.
- `[current]` — already up to date, so nothing was written.

For the marketplace harnesses (`claude-code`, `codex`) the plugin loads only
after you **restart the agent**, so an install/update row ends with
`(restart your agent to apply)`.
