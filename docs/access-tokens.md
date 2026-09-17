---
title: "Access tokens"
description: "Project-scoped credentials for CI and automation, minted and revoked from the CLI."
---

## What it is

A project access token (prefix `pt-`) is a credential that authenticates the
CLI against one project. It is the credential to put in CI: it reaches the
project it was minted in and nothing else, it carries a scope, and you can
revoke it without touching the rest of your account.

Your own account token (prefix `pk-`), the one `volcano login` saves, reaches
every project you own. Only an account token can manage access tokens.

## How it relates

- Belongs to a **project**, and authenticates that project's commands:
  functions, frontends, variables, databases, logs, and deploys.
- Cannot create, inspect, or revoke tokens, and cannot run account-wide
  commands: `volcano projects list`, `volcano projects create`, `volcano
  projects rename`, `volcano projects delete`, selecting a project by name, or
  `volcano git connect`. `volcano cloud access-tokens usage` is the exception:
  a token can report its own project's consumption, and its own day-by-day
  series by ID, so a CI job needs nothing but the credential it already runs
  with. `get --usage` is not, because reading one token's record is itself a
  token operation.
- Carries one of two scopes: `full` matches your own access to that project;
  `read_only` rejects writes.

## CLI operations

| Operation | Command |
|---|---|
| Create | `volcano cloud access-tokens create <name> [--scope <scope>] [--expires-at <timestamp>] [--json]` |
| List | `volcano cloud access-tokens list [--search <text>] [--include-revoked] [--json]` |
| Get | `volcano cloud access-tokens get <name-or-id> [--usage] [--days <n>] [--json]` |
| Usage | `volcano cloud access-tokens usage [<token-id>] [--days <n>] [--json]` |
| Revoke | `volcano cloud access-tokens revoke <name-or-id> [--yes]` |

`tokens` is an alias for `access-tokens`. These are cloud commands: local
development issues no credentials.

## Create a token

```bash
volcano cloud access-tokens create ci-deploy
```

```text
✓ Access token 'ci-deploy' created
ID: 7f1c2e94-2a6b-4c17-9a42-1b0c8f5d3e77
Scope: full
Expires: never

Token: pt-Wq9l2m4XcR7tFv1sN8bK3hJ0
Warning: Copy this token now. It is shown once and cannot be retrieved again.
```

The secret is returned only by `create`. Read commands show the token's prefix,
never the secret, so store it when you create it.

Scope the token down and give it an expiry when you can:

```bash
volcano cloud access-tokens create ci-audit \
  --scope read_only \
  --expires-at 2027-01-31T00:00:00Z
```

`--expires-at` takes an RFC3339 timestamp. Without it the token never expires.

The secret is printed once and never again. For a script, take it from `--json`
rather than parsing the human output:

```bash
secret=$(volcano cloud access-tokens create ci-deploy --json | jq -r .token)
```

## Set the right project

A project access token only works on the project it was created in, and the
token and the project are resolved separately: setting `VOLCANO_TOKEN` on a
machine that has already logged in leaves the project as whatever
`volcano use` selected last. If that is a different project, every command
returns a permission error — the CLI names the project it ran against so you
can tell that apart from a genuine permission problem.

## Use a token

Set it as `VOLCANO_TOKEN` along with the project it belongs to:

```bash
export VOLCANO_TOKEN=pt-Wq9l2m4XcR7tFv1sN8bK3hJ0
export VOLCANO_PROJECT_ID=eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550
volcano cloud functions deploy --all
```

Or log in with it, naming the project once:

```bash
volcano login --token pt-Wq9l2m4XcR7tFv1sN8bK3hJ0 --project eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550
```

A project access token cannot list projects, so nothing can work out which
project it belongs to, and nothing can turn a project name into an ID — name it
by ID, with `--project` or `VOLCANO_PROJECT_ID`.

Without either, `login` falls back to the project `volcano use` last selected,
which belongs to whatever credential was logged in before. If that is not the
token's project, the failure says where the project came from.

Run an account-wide command with one and the CLI says what is missing rather
than failing with a permission error:

```bash
volcano projects list
```

```text
Error: this command needs an account token (pk-) but the current credential is a
project access token (pt-), which only reaches the project it was minted in. Run
'volcano login', or set VOLCANO_TOKEN to an account token
```

## Inspect and revoke

```bash
volcano cloud access-tokens list
volcano cloud access-tokens get ci-deploy --usage --days 7
volcano cloud access-tokens revoke ci-deploy
```

`list` shows the tokens that can still authenticate. A token that stopped —
revoked, or past its `--expires-at` — is hidden until you ask for it, and then
reports which it was in the `Status` column:

```bash
volcano cloud access-tokens list --include-revoked
```

```text
Name                      Prefix            Scope       Status     Last used        Requests
ci-deploy                 pt-Wq9l2m4X       full        active     3h ago           42
ci-audit                  pt-4bN7sK1p       read_only   expired    20d ago          3
ci-old                    pt-Zx8c5Vt2       full        revoked    41d ago          77
```

`--usage` adds the token's daily request counts, zero-filled and oldest first,
so every day in the window is present:

```text
Day           Requests
------------------------
2026-09-14    18
2026-09-15    0
2026-09-16    24

42 request(s) over 3 day(s)
```

To compare tokens instead of days, `usage` totals the window for each one,
revoked tokens included:

```bash
volcano cloud access-tokens usage --days 7
```

```text
Name                      Requests
--------------------------------------
ci-deploy                 42
ci-audit                  3

45 request(s) across 2 token(s) over 7 day(s)
```

Both take `--days`, up to 60, defaulting to 30.

The `usage` reads are the ones a project access token can run for itself, so a
CI job can report what it consumed with nothing but the credential it already
holds — the whole project, or one token's day-by-day series:

```bash
export VOLCANO_TOKEN=pt-Wq9l2m4XcR7tFv1sN8bK3hJ0
export VOLCANO_PROJECT_ID=eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550
volcano cloud access-tokens usage --days 7
volcano cloud access-tokens usage 7f1c2e94-2a6b-4c17-9a42-1b0c8f5d3e77 --days 7
```

The series is addressed by token ID, the one `create` printed, because turning
a name into an ID needs the token list — an account operation. `get --usage`
reads the token's record first for the same reason, so it needs an account
token even for the running token's own counts.

Revoking takes effect immediately and breaks every pipeline still using the
token. The record is kept with status `revoked`, so the token keeps its history
and still appears under `--include-revoked` and in `usage`.
