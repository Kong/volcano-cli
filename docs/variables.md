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
  [declarative config](project-configuration.md). When declared in the
  manifest, variables are **fully synced** on `config deploy`: entries absent
  from the manifest are deleted.

## CLI operations

| Operation | Command |
|---|---|
| Deploy from env file | `volcano cloud variables deploy [-f <path>]` |
| List | `volcano cloud variables list` |
| Get | `volcano cloud variables get <name>` |
| Delete | `volcano cloud variables delete <name>` |

Without `cloud`, these commands act on local development (`volcano start`), not
on your cloud project. When local development is not running they fail rather
than fall back to the cloud.

`deploy` reads variables from the file only; `NAME=value` arguments are an
error. It creates and updates variables and never deletes them.

## Examples

```bash
# Deploy variables from volcano/volcano.env (or a custom file)
volcano cloud variables deploy
volcano cloud variables deploy -f ./secrets.env

# Inspect and remove
volcano cloud variables list
volcano cloud variables get STRIPE_SECRET_KEY
volcano cloud variables delete STRIPE_SECRET_KEY

# The same against local development
volcano variables deploy
```

Deploying variables triggers a rollout to the affected functions and frontends.
