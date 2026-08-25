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

Every plan includes branching: a database may hold 10 branches on Free and 25 on
Pro, counting branches in every state. Local development projects have no
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

Branch names are lowercase letters, numbers, and underscores, up to 64
characters, and unique within their parent database.

A branch has its own connection string with its own credentials, separate from
the parent's. It is returned before it is ready, so fetch the branch until it
reports `active`:

```bash
# Fork "app" into a branch that lives for a day
volcano cloud databases branches create app feature_x --ttl 24h

# Poll until it is connectable, then read its connection string
volcano cloud databases branches get app feature_x --show-connection-string
```

`reset` discards everything the branch has diverged by and re-forks it from the
parent's current state, keeping the branch's name and credentials. It rewinds in
the background, so the branch goes back to `provisioning` and stops serving
connections until it reports `active` again — poll it with `get` the same way you
would after a `create`. `extend` replaces a branch's lifetime rather than adding
to it, counting from now.

## Backups and restore

A backup is a point-in-time copy of a database, kept by the platform and
restorable in place. Backups cover the database itself, not its branches.

Backups are a Pro capability. On the Free plan every command below fails with
`403`, reads included:

```console
$ volcano cloud databases backups list app
Error: backups are not available on this plan
```

Like branching, backups have no local backend, so these commands live under the
`cloud` group and the prefix is required. Dropping it points you back at the
cloud path rather than running anything:

```console
$ volcano databases backups list app
Error: "backups" is a cloud command: local development has no storage provider
behind it, so run 'volcano cloud databases backups' against a cloud project
```

| Operation | Command |
|---|---|
| Create | `volcano cloud databases backups create <database> <backup>` |
| List | `volcano cloud databases backups list <database>` |
| Get | `volcano cloud databases backups get <database> <backup>` |
| Delete | `volcano cloud databases backups delete <database> <backup> [--yes]` |
| Restore | `volcano cloud databases restore <database> --backup <backup> [--yes]` |
| Restore to a point in time | `volcano cloud databases restore <database> --to <RFC 3339> [--yes]` |
| List restores | `volcano cloud databases restores list <database>` |
| Show one restore | `volcano cloud databases restores get <database> <restore-id>` |
| Show the schedule | `volcano cloud databases backup-schedule get <database>` |
| Set the schedule | `volcano cloud databases backup-schedule set <database> --frequency <daily\|weekly\|monthly> [--hour 3] [--day 1] [--retention 168h]` |
| Stop scheduled backups | `volcano cloud databases backup-schedule set <database> --clear` |

`backups` is also spelled `backup`. `delete` and `restore` ask for confirmation
first; pass `--yes` (`-y`) to skip the prompt in a script. `--day` is the day of
the week for a weekly schedule (`1`-`7`, Monday to Sunday) and the day of the
month for a monthly one (`1`-`28`); it is required for both and ignored by a
daily schedule.

Whether a database may be backed up at all, how many backups it may keep, how
long they are kept, and how far back a point-in-time restore reaches all come
from the project's plan. `backups list` reports the window a point-in-time
restore may target.

What the backups hold counts against the parent database's storage allowance,
and a backup you take is charged as a full copy of the database as it was then.
A schedule's first backup is charged the same way and each one after it only for
the storage it adds, so the `Size` column — how much data a backup holds — runs
ahead of what the backup costs to keep. Deleting a backup releases its storage
straight away. A plan without backups is charged nothing for the ones it still
holds after a downgrade.

```bash
# Back up before a risky migration
volcano cloud databases backups create app before_migration

# See what there is to restore, and how far back a point-in-time restore reaches
volcano cloud databases backups list app

# Put the database back the way that backup found it
volcano cloud databases restore app --backup before_migration

# Or rewind to an arbitrary moment inside the window, without the prompt
volcano cloud databases restore app --to 2026-01-15T09:30:00Z --yes

# Watch it, using the id the restore printed
volcano cloud databases restores get app 6f1c…
```

Restoring is destructive and in place: everything written after the point being
restored is discarded, and there is no way to restore into a second database.
The restore runs in the background, so the command returns while the database is
still `restoring` and serving no connections. Its connection string never
changes, so nothing holding it needs updating.

Watch it with `restores get`, which the restore command prints the id for. A
`pending` or `running` restore is still going, and an attempt that fails with
tries left goes back to `pending`; `failed` and `exhausted` both mean the platform
gave up. Either leaves the database `failed` if an attempt had already begun
replacing its data, and `active` if none had — a backup that no longer exists at
the provider ends the restore without touching the database. Only the restore
carries the reason —
the database itself reports the status and nothing more. `restores list` shows
the 50 most recent restores of a database, newest first.

While a restore runs, the commands that would race it are refused: another
`restore`, `backups create`, `backups delete`, `backup-schedule set`, `databases
delete`, and `branches create` or `branches reset`. Wait for the database to
report `active` and retry.

Branches are not restored. They keep serving their own data, but resetting a
branch from a database that was just restored is refused for up to 24 hours
afterwards.

`backup-schedule set` replaces the schedule rather than adding to it, and
scheduled backups do not count against the plan's backup allowance. Their
retention is clamped to the plan's, so the schedule printed back can keep
backups for less time than asked for. Scheduled backups are listed alongside the
ones you took, with a source of `scheduled`, and are restored and deleted the
same way.

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
