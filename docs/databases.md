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
| Create | `volcano databases create <name> [--type …] [--region aws-<aws-region>] [--pg-version …]` |
| List | `volcano databases list` |
| Get | `volcano databases get <name> [--show-connection-string]` |
| Delete | `volcano databases delete <name>` |
| Apply migrations (per db) | `volcano databases migration up …` |
| Apply migrations (top-level) | `volcano migrations deploy --all -d <db>` |

Prefix with `cloud` to force the cloud target.

## Branches

A branch is a short-lived copy-on-write fork of a database you can develop and
test against. It starts as a copy of the parent and diverges from there: writes
to a branch never reach the parent.

Every branch expires — the default lifetime is 7 days, and `--ttl` accepts
anything between `1h` and `720h`. Only the data a branch has diverged by counts
against the parent database's storage allowance, so a fresh branch is free.

Branching is available on PRO projects. Local development projects have no
branching backend, so unlike the commands above these live under the `cloud`
group and the prefix is required rather than optional.

| Operation | Command |
|---|---|
| Create | `volcano cloud databases branches create <database> <branch> [--ttl 24h]` |
| List | `volcano cloud databases branches list <database>` |
| Get | `volcano cloud databases branches get <database> <branch> [--show-connection-string]` |
| Extend | `volcano cloud databases branches extend <database> <branch> --ttl <duration>` |
| Reset | `volcano cloud databases branches reset <database> <branch>` |
| Rotate password | `volcano cloud databases branches rotate-password <database> <branch>` |
| Delete | `volcano cloud databases branches delete <database> <branch>` |

A branch has its own connection string with its own credentials, separate from
the parent's. It is returned before it is ready, so fetch the branch until it
reports `active`:

```bash
# Fork "app" into a branch that lives for a day
volcano cloud databases branches create app feature-x --ttl 24h

# Poll until it is connectable, then read its connection string
volcano cloud databases branches get app feature-x --show-connection-string
```

`reset` discards everything the branch has diverged by and re-forks it from the
parent's current state, keeping the branch's name and credentials. `extend`
replaces a branch's lifetime rather than adding to it, counting from now.

## Examples

```bash
# Create a database (defaults to type volcano-db-xs)
volcano databases create app
volcano databases create app --type volcano-db-s --region aws-us-east-1

# Show the connection string
volcano databases get app --show-connection-string

# Apply migration files from volcano/migrations to the "app" database
volcano migrations deploy --all -d app
```
