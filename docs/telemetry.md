---
title: "Telemetry"
description: "What usage data the Volcano CLI collects, how it is sent, and how to turn it off."
---

## What it is

The Volcano CLI sends one usage event to the Volcano API each time you run a
command. The Volcano team uses it to see which commands people use, where they
fail, and which coding agents run them. Telemetry is on by default. The first
time you run a command, the CLI prints a notice that explains this and sends
nothing, so you can turn telemetry off before any event is sent.

## What is collected

| Field | Example |
|---|---|
| Command | `cloud functions deploy` |
| Names of the flags you set | `all`, `debug` |
| Selected project ID | UUID |
| Resource names and IDs the command was given | `database: app`, `function: hello` |
| CLI version | `v0.47.0` |
| Operating system and CPU architecture | `darwin`, `arm64` |
| Number of CPUs | `10` |
| CI vendor, when run in CI | `github-actions` |
| Whether stdin is a terminal | `true` |
| Coding agent that ran the command, how it was found, and every agent detected | `claude-code`, `marker`, `claude-code,cursor` |
| Whether the Volcano plugin is installed for that agent, and its version | `true`, `0.2.16` |
| Exit code and duration | `1`, `5321` ms |
| A random ID for this machine, and one for this run | UUIDs |

When a command fails, the event also carries the type of failure (`api`,
`network`, `local_not_running`, `usage`, or `other`), the HTTP status and error
code of an API error, and the error message. Before sending the message, the
CLI replaces your home directory with `~`, removes bearer tokens, JWTs, Volcano
keys (`pk-`, `pt-`, `sk-`, `ak-`), URL query strings, and other long opaque
strings, and cuts it to 500 characters.

Resource names come from the arguments that name a resource, such as
`<database>` in `volcano databases get <database>`, and from the `--name`,
`--function`, `--database`, `--project`, `--topic`, and `--bucket` flags. Other
arguments and flag values, such as file paths, search queries, payloads, and
commands run in a sandbox, are never sent. A name that looks like a token or
key is dropped.

The CLI never sends file contents or environment variable values, with one
exception: the agent name and its raw value come from `AI_AGENT`, `AGENT`, or
`VOLCANO_AGENT` when one of those is set. The CLI limits them to letters,
digits, and `._-/@+:` and to 64 characters, and the Volcano API forwards only
known agent names to analytics and reports any other as `other`. See
[agent-detection.md](agent-detection.md).

If you are logged in, the event is linked to your Volcano account. Otherwise it
is linked only to the random machine ID, stored in `~/.volcano/config.json`.

Shell completion (pressing Tab), commands run with `--offline`, and `volcano
telemetry` commands are never recorded.

## How it is sent

The CLI starts a short background process that posts the event to the Volcano
API, so commands do not wait on the network. The API checks every field against
a fixed list or pattern before it stores the event, and drops anything else.

## Turn it off

Use either of these:

```bash
volcano telemetry disable    # saved in ~/.volcano/config.json
export VOLCANO_TELEMETRY_DISABLED=1
```

The CLI reads the setting when a command starts, so a change applies from the
next command. `volcano telemetry status` shows the current setting, and
`volcano telemetry enable` turns it back on. Logging out does not change the
setting.

## See what is sent

Set `VOLCANO_TELEMETRY_DEBUG=1` to print each event to stderr instead of
sending it. It is off unless set:

```bash
VOLCANO_TELEMETRY_DEBUG=1 volcano functions list
```
