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

On macOS and Linux, `-f` also takes a process substitution, so a secrets
manager's output can be deployed without writing it to disk:

```bash
volcano cloud variables deploy -f <(op inject -i volcano.env.tpl)
```

Through the npm-installed CLI, this works from bash, and on macOS it needs
Node.js 22.15 or later. zsh's process substitution doesn't reach the
npm-installed CLI.

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

[`volcano deploy`](deploy.md) deploys the env file first, before the code
that reads it. It refuses when a manifest also declares `variables`, because
applying that section would delete the variables only the env file sets.
