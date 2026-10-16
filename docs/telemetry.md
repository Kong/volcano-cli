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
| Selected project ID, for `volcano cloud` commands | UUID |
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
CLI replaces your home directory with `~`, strips terminal color codes, and
removes bearer tokens, JWTs, Volcano keys (`pk-`, `pt-`, `sk-`, `ak-`), AWS
access key IDs, passwords in URLs, URL query strings, values of `password=`,
`token=`, `DB_PASSWORD=`, and similar pairs, JSON fields such as `"token":
"..."`, and other long opaque strings. When a secret's value is quoted, or is a
JSON field, everything after it in the message is removed too. It then cuts the
message to 500 characters.

Some errors can quote your files or data, so the CLI replaces that part with
`[redacted]`:

- reading or validating `volcano-config.yaml`
- reading an env file such as `volcano.env`
- a failed database migration, whose PostgreSQL error can quote row values
- the error a function returns to `volcano functions invoke`, which can hold
  your application's data; only its HTTP status is sent

An error message can contain what you typed, such as an unknown command, a flag
value the CLI rejected, a file path, or a resource name. Redaction removes the
secret shapes listed above but cannot recognize every secret, so avoid putting
secrets in command arguments.

Resource names come from the arguments that name a resource, such as
`<database>` and `<branch>` in `volcano databases branch get <database>
<branch>`, and from the `--name`, `--function`, `--database`, `--project`,
`--topic`, and `--bucket` flags. `volcano durable start --name` is not sent,
since it is an idempotency key that can hold your application's data. Other arguments and flag values, such as file
paths, search queries, payloads, and commands run in a sandbox, are not sent as
resource names, though they can appear in an error message. A name that looks
like a path, token, or key is dropped.

The CLI does not send file contents or environment variable values, except in
two cases. The selected cloud project ID can come from `VOLCANO_PROJECT_ID`. The agent
name and its raw value come from `AI_AGENT`, `AGENT`, or `VOLCANO_AGENT` when
one of those is set. The CLI limits them to letters,
digits, and `._-/@+:` and to 64 characters, and the Volcano API forwards only
known agent names to analytics and reports any other as `other`. See
[agent-detection.md](agent-detection.md).

If you are logged in, the event is linked to your Volcano account. Otherwise it
is linked only to the random machine ID, stored in `~/.volcano/config.json`.

Shell completion (`volcano completion` and pressing Tab), commands run with
`--offline`, and `volcano telemetry` commands are never recorded.

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
