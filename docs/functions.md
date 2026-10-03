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
- Reads configuration and secrets from **variables**. By default it receives
  every project variable; declare a narrower
  [variable scope](project-configuration.md) to give it only the ones it needs.
- Connects to **databases** and **storage** in the same project.
- Its **visibility** (`private`, `authenticated`, or `public`) and
  **schedulers** can be declared in the
  [declarative config](project-configuration.md) or managed with the CLI.
- A **frontend route** can serve a public function under a path of a
  [frontend](frontends.md).
- Can be given an **alias** so you can invoke it by a friendly name.
- Work that has to run for hours belongs in a
  [durable function](durable-functions.md) instead, which is a separate
  collection with its own commands.

The CLI discovers functions in the `volcano/functions` directory, detects the
runtime from source file extensions, and uploads a packaged archive.

If a `volcano-config.yaml` is present, `functions deploy` also sends the
`variable_scope` and `variables` declared there for each function it uploads.
Functions the manifest does not mention keep their existing scope. The manifest
is only read — deploying never writes it back. See
[variable scope](project-configuration.md).

Because those declarations come from the manifest, a manifest that is present
but cannot be read stops the deploy before anything is uploaded, and the error
says what to fix. That includes a `${VAR}` reference anywhere in the file whose
environment variable is not set in the deploying shell, even in a section
unrelated to functions. Deploying past it would upload a function without the
scope it declared, giving it every project variable. With no
`volcano-config.yaml` at all there is nothing to declare and deploy runs
normally.

## CLI operations

| Operation | Command |
|---|---|
| Deploy one or all | `volcano functions deploy [-a \| -f <name\|path>]` |
| List | `volcano functions list` |
| Get | `volcano functions get <name>` |
| Set visibility | `volcano functions update <name> --visibility private\|authenticated\|public` |
| Delete | `volcano functions delete <name>` |
| Invoke | `volcano functions invoke <name> [--payload …] [--json]` |
| Logs | `volcano functions logs <name>` |
| Supported runtimes | `volcano functions runtimes` |
| Aliases | `volcano functions alias set\|list\|delete …` |
| Schedulers | `volcano functions schedulers create\|list\|enable\|disable\|delete …` |

Prefix with `cloud` (e.g. `volcano cloud functions deploy`) to target cloud.
Without the prefix, function commands always target local development.

Cloud deploys use latest-wins queueing. If the function is already deploying,
the new source replaces any older queued deploy and runs next. Delete supersedes
queued deploys; later deploys are rejected until deletion finishes.

## Visibility

Visibility decides who can invoke a function:

| Visibility | Who can invoke it |
|---|---|
| `private` | Service keys and schedulers |
| `authenticated` | Also your project's signed-in users |
| `public` | Also anon keys with `functions.invoke`, and [frontend routes](frontends.md#function-routes) |

New functions start `private`. `functions deploy` says so for each function it
creates and prints the command that lets signed-in users in:

```text
  Deployed notes-summary (new, visibility private)

New functions are private: only service keys and schedulers can invoke them.
To let your project's signed-in users call one, run:
  volcano functions update notes-summary --visibility authenticated
```

`--public` is the same as `--visibility public`. `--private` is no longer
accepted: it used to let signed-in users in, which is `authenticated` now, so
pick the level you mean. A function that a frontend route forwards to has to
stay `public`; delete the route first.

`functions get` shows the level, and under "Routed from" every frontend path
that forwards to the function:

```text
Visibility: public
Routed from: web /api/session
```

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

# Let signed-in users invoke a function
volcano functions update notes-summary --visibility authenticated

# Tail build/runtime logs
volcano functions logs hello

# Schedule a function (cron), then disable it
volcano functions schedulers create hello --name refresh-cache --cron "*/5 * * * *"
volcano functions schedulers disable hello --name refresh-cache

# Invoke-by-alias
volcano functions alias set hello 44444444-4444-4444-8444-444444444444
volcano functions invoke hello
```
