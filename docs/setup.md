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

On a real terminal, `setup` shows a picker of detected harnesses (an installed
harness that is behind its latest version is marked `[outdated]`). It runs
non-interactively — installing all detected — when stdin/stdout is piped, in CI,
when `VOLCANO_NONINTERACTIVE` is set, or with `--yes`.

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
