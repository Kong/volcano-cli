---
title: "Databases"
description: "A managed PostgreSQL database provisioned for your project. Functions connect to it using its connection string."
---

## What it is

A managed PostgreSQL database provisioned for your project. Functions connect to
it using its connection string.

## How it relates

- Belongs to a **project**.
- **Functions** read and write it via its connection string.
- Its schema is evolved by **migrations** — SQL files in `volcano/migrations`
  applied to a named database.
- Database *requirements* can also be declared in the
  [declarative config](project-configuration.md) (the manifest updates
  settings but never creates or deletes databases).

## CLI operations

| Operation | Command |
|---|---|
| Create | `volcano databases create <name> [--type …] [--region …] [--pg-version …]` |
| List | `volcano databases list` |
| Get | `volcano databases get <name> [--show-connection-string]` |
| Delete | `volcano databases delete <name>` |
| Apply migrations (per db) | `volcano databases migration up …` |
| Apply migrations (top-level) | `volcano migrations deploy --all -d <db>` |

Prefix with `cloud` to force the cloud target.

## Examples

```bash
# Create a database (defaults to type volcano-db-xs)
volcano databases create app
volcano databases create app --type volcano-db-s --region us-east-1

# Show the connection string
volcano databases get app --show-connection-string

# Apply migration files from volcano/migrations to the "app" database
volcano migrations deploy --all -d app
```
