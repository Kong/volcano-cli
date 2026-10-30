---
title: "Logs"
description: "Search, tail and count a project's runtime logs across its functions, frontends or databases."
---

## What it is

`volcano logs` reads the runtime logs of every resource of one type in the
current project, newest first, or of the ones you name. `volcano logs activity`
counts them over time. Both take the same filters.

```bash
volcano cloud logs --since 1h --level error
```

```text
2026-10-30T09:14:02Z [us-east-1] error checkout: payment provider timed out
2026-10-30T09:13:48Z [us-east-1] error checkout: payment provider timed out
```

Build logs stay on each resource: `volcano functions logs <name> --type build`,
`volcano durable logs <name> [deployment-id]` and
`volcano frontends logs <name> --type build`.

## Choosing what to read

| Flag | Reads |
|---|---|
| `--type function` (default) | Functions, standard and durable |
| `--kind standard` or `--kind durable` | Only that kind of function |
| `--type frontend` | Frontends |
| `--type database` | Databases (on plans that include database logs) |
| `--name <name>` | Only the named resources; repeat or separate with commas, up to 25 |

One read covers one type. A function or frontend is named by name or ID, a
database by name.

## Filtering

| Flag | Keeps |
|---|---|
| `-q`, `--query <query>` | Logs matching a query: quoted text, `AND`, `OR`, `NOT`, parentheses, and fields such as `level:error` or `resource.name:checkout` |
| `--level <level>` | One level: `trace`, `debug`, `info`, `warn`, `error` or `fatal` |
| `--region <region>` | Logs from one region, such as `us-east-1` |
| `--invocation <id>` | Logs from one function invocation |
| `--since <duration>` | Logs newer than `15m`, `2h`, `7d` and so on |
| `--start <time>`, `--end <time>` | Logs in an RFC 3339 window |

Filters combine with AND:

```bash
volcano cloud logs --name checkout --query 'timeout OR "connection reset"' --region us-east-1
```

History is limited to the plan's log retention; an older window is clamped to
it.

## Paging, tailing and JSON

`--limit` (default 100) is how many logs to print. When more match, the command
says so on stderr; raise `--limit` or narrow the window.

`--follow` tails new logs as they arrive, after printing up to `--limit` recent
ones. Press Ctrl+C to stop. `--end` cannot be combined with it.

`--json` prints one JSON object per log, with its level, region, resource and
invocation, so a script can read a page or a tail the same way:

```bash
volcano cloud logs --type frontend --name web --json --limit 1
```

```json
{"body":"GET /checkout 502","id":"evt_01J...","invocation_id":"9b1d...","level":"error","region":"us-east-1","resource":{"id":"5b6c...","name":"web","type":"frontend"},"timestamp":"2026-10-30T09:14:02Z"}
```

## Counting logs over time

```bash
volcano cloud logs activity --since 6h --buckets 6
```

```text
2026-10-30T04:00:00Z      12  info 12
2026-10-30T05:00:00Z      31  error 4, info 27
...

43 logs
```

The window defaults to the last 24 hours, split into at most `--buckets`
buckets (default 24, at most 96). `--json` prints each bucket's counts by level,
region and resource ID.

## Local mode

Both commands work against the local stack with the top-level path:
`volcano logs` and `volcano logs activity`.
