---
title: "Functions"
description: "A function is a unit of deployed backend logic (JavaScript/TypeScript, Python, or Ruby) that Volcano builds, packages, and runs on a managed runtime."
---

## What it is

A function is a unit of deployed backend logic (JavaScript/TypeScript, Python,
or Ruby) that Volcano builds, packages, and runs on a managed **runtime**. It
is invoked over HTTP, by name/alias, or on a schedule.

## How it relates

- Belongs to a **project**.
- Reads configuration and secrets from **variables**.
- Connects to **databases** and **storage** in the same project.
- Its **visibility** (public/private) and **schedulers** can be declared in the
  [declarative config](project-configuration.md) or managed with the CLI.
- Can be given an **alias** so you can invoke it by a friendly name.

The CLI discovers functions in the `volcano/functions` directory, detects the
runtime from source file extensions, and uploads a packaged archive.

## CLI operations

| Operation | Command |
|---|---|
| Deploy one or all | `volcano functions deploy [-a \| -f <name\|path>]` |
| List | `volcano functions list` |
| Get | `volcano functions get <name>` |
| Update settings | `volcano functions update <name> …` |
| Delete | `volcano functions delete <name>` |
| Invoke | `volcano functions invoke <name> [--payload …] [--json]` |
| Logs | `volcano functions logs <name>` |
| Supported runtimes | `volcano functions runtimes` |
| Aliases | `volcano functions alias set\|list\|delete …` |
| Schedulers | `volcano functions schedulers create\|list\|enable\|disable\|delete …` |

Prefix with `cloud` (e.g. `volcano cloud functions deploy`) to force the cloud
target instead of the active context.

Cloud deploys use latest-wins queueing. If the function is already deploying,
the new source replaces any older queued deploy and runs next. Delete supersedes
queued deploys; later deploys are rejected until deletion finishes.

## Examples

```bash
# Deploy everything, or a single function by name or path
volcano functions deploy --all
volcano functions deploy -f get-notes
volcano functions deploy -f volcano/functions/get-notes.js

# Invoke with a JSON payload; --json prints compact machine output
volcano functions invoke hello --payload '{"name":"Ada"}'
volcano functions invoke hello --json
volcano functions invoke --id 33333333-3333-4333-8333-333333333333

# Tail build/runtime logs
volcano functions logs hello

# Schedule a function (cron), then disable it
volcano functions schedulers create hello --name refresh-cache --cron "*/5 * * * *"
volcano functions schedulers disable hello --name refresh-cache

# Invoke-by-alias
volcano functions alias set hello 44444444-4444-4444-8444-444444444444
volcano functions invoke hello
```
