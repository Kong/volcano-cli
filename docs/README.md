---
title: "CLI operations overview"
description: "This is a map of the Volcano building blocks (\"elements\") the CLI operates on, how they relate, and where the CLI can act on each."
---

This is a map of the Volcano building blocks ("elements") the CLI operates on,
how they relate, and where the CLI can act on each. Each element has its own
page with concrete command examples. For exact, always-current flags, run
`volcano <command> --help`.

## The model

A **project** is the container for everything else. You authenticate, select an
active project, then operate on the resources inside it:

```text
account (auth)
└── project ................... volcano projects / use
    ├── functions ............. deployed backend logic        → functions.md
    │     ├── uses → variables (config & secrets)
    │     ├── connects → databases, storage
    │     ├── runs on → a runtime
    │     ├── invoked via → alias / HTTP / scheduler
    ├── durable functions ..... long-running, checkpointed logic → durable-functions.md
    ├── databases ............. managed Postgres               → databases.md
    │     └── schema changed by → migrations
    ├── storage ............... buckets → objects, policies    → storage.md
    ├── variables ............. env vars / secrets             → variables.md
    ├── frontends ............. deployed sites, custom domains → frontends.md
    ├── git connection ........ push-to-deploy from GitHub     → git.md
    ├── access tokens ......... project-scoped CI credentials  → access-tokens.md
    └── declarative config .... one YAML syncing the above     → ../project-configuration.md
```

Cross-cutting concerns:

- **Authentication** gates all cloud access — see [authentication.md](authentication.md).
- **Declarative configuration** applies settings across every element from one
  manifest — see [project-configuration.md](project-configuration.md).
- **Git connection** lets a push deploy the project without a CLI invocation —
  see [git.md](git.md).

## Local vs. cloud target

The CLI can operate against two targets:

- **Local** development environment (`volcano start` / `stop` / `restart` /
  `status` / `reset`), targeted by top-level resource commands.
- **Cloud** project — targeted with the `cloud` prefix (e.g. `volcano cloud
  functions list`).

Top-level resource commands (`volcano functions …`, `volcano durable …`,
`volcano databases …`, etc.) always target local development. They fail if the
local environment is not running; they do not fall back to cloud. Use the
explicit `volcano cloud …` prefix for cloud operations.

## Output styling

Command output is colorized to share one identity with `volcano setup`: success
marks and active states in orange, warnings in amber, errors in red, table
headers and titles in lava, and suggested commands in flame. Color is written
only to an interactive terminal. It is disabled automatically when output is
piped or redirected, in CI, or when `NO_COLOR` is set, so machine-read output
(including `--json`) stays plain.

```bash
volcano databases list          # colorized on a terminal
volcano databases list | cat    # plain (piped)
NO_COLOR=1 volcano databases list
```

## Account credit notices

Cloud commands can report when your account is running low on credits or has
no credits left. The notice includes the billing page address when available.
Open that page to add credits. These notices do not prompt or open a browser.
Piped, CI, and `NO_COLOR` output remains plain. Machine output is unchanged.

## Element → CLI ability at a glance

| Element | CLI can… | Commands | Details |
|---|---|---|---|
| Account / auth | sign up, log in/out | `signup`, `login`, `logout` | [authentication.md](authentication.md) |
| Access tokens | create, list, get, revoke project credentials | `projects keys access-tokens …`, `cloud access-tokens …` | [access-tokens.md](access-tokens.md) |
| Project | create, list, get, rename, delete, select, get keys and usage | `projects …`, `use` | below |
| Functions | deploy, invoke, inspect, schedule, alias | `functions …` | [functions.md](functions.md) |
| Durable functions | deploy, start, inspect executions, read logs, schedule | `durable …`, `cloud durable …` | [durable-functions.md](durable-functions.md) |
| Databases | create, inspect, delete, migrate | `databases …`, `migrations …` | [databases.md](databases.md) |
| Storage | manage buckets, objects, policies | `storage …` | [storage.md](storage.md) |
| Variables | deploy, list, get, delete | `variables …` | [variables.md](variables.md) |
| Frontends | deploy, inspect, custom domains | `cloud frontends …` | [frontends.md](frontends.md) |
| Config (declarative) | deploy/pull one YAML manifest | `config deploy`, `config pull` | [project-configuration.md](project-configuration.md) |
| Local environment | check prerequisites, start/stop/status/reset | `doctor`, `start`, `stop`, `restart`, `status`, `reset` | above |
| Coding agents | install Volcano skills/plugins, keep them current | `setup` | [setup.md](setup.md) |
| Documentation | search/read the docs | `docs …` | [docs-search.md](docs-search.md) |

## Projects

A project owns all resources. Authentication and an active project are
prerequisites for cloud operations.

```bash
volcano projects create my-app     # create a project
volcano projects list              # list your projects
volcano use my-app                 # set the active project
volcano projects get               # details for the active project
volcano projects rename eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550 new-name  # rename a project
volcano projects keys anon list    # anon (publishable) API keys for the browser/SDK
volcano projects keys service list # backend service-key metadata for the active project
volcano projects keys access-tokens list # project access tokens for CI
volcano projects usage             # current-month and all-time usage totals
volcano projects delete my-app     # delete
```

`VOLCANO_PROJECT_ID` overrides the active project for a single invocation
(useful in CI).

### Project keys

`volcano projects keys` requires an explicit key type: `anon`, `service`, or
`access-tokens`. See [access-tokens.md](access-tokens.md) for token operations.
`volcano projects keys anon list [project-id]` lists publishable anon keys.
`volcano projects keys anon create <name> [project-id]` creates a publishable
auth-only key using the server default.

### Backend service keys

Service keys are privileged backend credentials that bypass row-level security.
Never expose their values in frontend code, logs, shell history, or other
untrusted output. `list` and `get` show metadata by default. Use `--show-key`
only when you need the plaintext. `create` prints the new key; use `--json`
to script it:

```bash
volcano projects keys service list --page 1 --limit 100
volcano projects keys service create worker --permission functions.invoke --json
volcano projects keys service get <key-id> --show-key
```

`--permission` is repeatable; `--permissions` is an equivalent spelling and
the two can be combined. Omitting both gives the key full access. Empty or blank
permission values are rejected so an intended restricted key cannot silently
fall back to full access.

An explicit `[project-id]` takes precedence over `VOLCANO_PROJECT_ID` and the
active project. Without it, `VOLCANO_PROJECT_ID` takes precedence over the
active project. Paginated list output includes a next-page command that retains
an explicit project ID.

### Project usage

`volcano projects usage [project-id]` prints each metric's current-month total
and all-time total. It does not print the hourly or daily time series returned
by the API. Project selection follows the same explicit ID, environment, then
active-project precedence described above.

## Starting a project

```bash
volcano init javascript            # scaffold a local project (js/nextjs/python/ruby)
volcano start                      # start the local dev environment
volcano status                     # check it
```

`volcano start` runs the local stack from a bundled default server image.
Override it with `--image` or `VOLCANO_IMAGE`; an explicitly selected image must
already exist locally, since the CLI never pulls an unpublished local-mode image.

```bash
volcano start --image kong/volcano:local-nightly
VOLCANO_IMAGE=kong/volcano:local-nightly volcano start
```
