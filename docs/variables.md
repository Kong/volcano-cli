---
title: "Variables"
description: "Environment variables (including secrets) made available to your project's functions and frontends at runtime."
---

## What it is

Environment variables (including secrets) made available to your project's
functions and frontends at runtime.

## How it relates

- Belongs to a **project**.
- Consumed by **functions** and **frontends** at runtime.
- Sourced from an env file (`volcano/volcano.env` or `./volcano.env`) or the
  [declarative config](project-configuration.md). When declared, variables
  are **fully synced** on deploy: entries absent from the source are deleted.

## CLI operations

| Operation | Command |
|---|---|
| Deploy from env file | `volcano variables deploy [-f <path>]` |
| List | `volcano variables list` |
| Get | `volcano variables get <name>` |
| Delete | `volcano variables delete <name>` |

Prefix with `cloud` to force the cloud target.

`variables deploy` only takes values from the env file (or `--file`); it does
not accept `KEY=value` positional arguments. Passing any positional value,
e.g. `volcano variables deploy API_KEY=value`, fails before the file is read.

## Examples

```bash
# Deploy variables from volcano/volcano.env (or a custom file)
volcano variables deploy
volcano variables deploy -f ./secrets.env

# Inspect and remove
volcano variables list
volcano variables get STRIPE_SECRET_KEY
volcano variables delete STRIPE_SECRET_KEY
```

Deploying variables triggers a rollout to the affected functions and frontends.
