---
title: "Deployments"
description: "List, inspect and summarize the deployments of a project's functions, durable functions and frontends from the CLI."
---

`volcano deployments` reads the project's deployment history: every deploy of
a function, durable function or frontend, whatever started it — the CLI, a Git
push, the dashboard or the API.

```bash
volcano cloud deployments list --since 1d
volcano cloud deployments get 8f0c2a1e-5b7d-4c6e-9a3f-2d1b0e4c7a91
volcano cloud deployments summary --type frontend --since 7d
```

Without `cloud`, the commands read the local environment's deployments.

## List deployments

`deployments list` prints the newest deployments first:

```bash
volcano cloud deployments list
```

```text
Created               Resource                      Op        Status        Source  Duration  ID
12m ago               function checkout             deploy    active        cli     1m32s     8f0c2a1e-5b7d-4c6e-9a3f-2d1b0e4c7a91
31m ago               durable function pipeline     deploy    failed        git     48s       0b6d9e2f-7c1a-4f3b-8d5e-6a2c4b1f9e07
16h ago               frontend web                  deploy    active        api     2m5s      c41e8a3b-2d6f-4e9c-a1b7-5f3d0c8e2a64

Total: 3 deployment(s)
```

| Flag | Effect |
|---|---|
| `--type function\|frontend` | Only one resource type. Both by default. |
| `--kind standard\|durable` | Only standard or durable functions. Implies `--type function`. |
| `--name <text>` | Only resources whose name contains the text. |
| `--since <duration>` | Only deployments newer than this, such as `2h` or `7d`. |
| `--after <time>` | Only deployments created at or after an RFC 3339 time. |
| `-l, --limit <n>` | Maximum deployments to list. Default 20. |
| `--json` | Print a JSON array of deployments. |

When more deployments match than `--limit` allows, the listing says so. Pass a
larger limit or narrow the filters.

## Inspect a deployment

`deployments get` shows one deployment by ID: the resource, status, trigger,
start and finish times, each build phase, any error, and the command that
prints its build logs.

```bash
volcano cloud deployments get 8f0c2a1e-5b7d-4c6e-9a3f-2d1b0e4c7a91
```

```text
ID: 8f0c2a1e-5b7d-4c6e-9a3f-2d1b0e4c7a91
Resource: function checkout (33333333-3333-4333-8333-333333333333)
Operation: deploy
Status: failed
Trigger: cli
Created: 2026-10-30T09:00:00Z
Completed: 2026-10-30T09:01:32Z
Duration: 1m32s
Error: build failed: npm ERR! missing script

Phase           Status        Duration
queue           succeeded     2s
build           failed        1m20s

Build logs: volcano cloud functions logs checkout 8f0c2a1e-5b7d-4c6e-9a3f-2d1b0e4c7a91 --type build
```

`get` searches the project's deployment history, newest first, and stops after
5,000 deployments. Pass `--json` for the full record.

## Summarize outcomes

`deployments summary` counts one resource type's deployments: how many there
were, succeeded, failed and were superseded, the success rate, and the median
build time. Superseded deployments count toward neither the rate nor the
median.

```bash
volcano cloud deployments summary --since 7d
```

```text
Deployments: 42
Succeeded: 38
Failed: 3
Superseded: 1
Success rate: 92.7%
Median build: 1m12s
```

`--type` defaults to `function`. The other filters match `deployments list`.
Pass `--json` for the raw counts.

## Related

- [functions.md](functions.md) — `functions logs <name> <deployment-id> --type build`
- [frontends.md](frontends.md) — `frontends logs <name> <deployment-id> --type build`
- [durable-functions.md](durable-functions.md)
- [scripting.md](scripting.md) — JSON output and exit codes
