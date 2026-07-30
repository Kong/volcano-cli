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
    ├── databases ............. managed Postgres               → databases.md
    │     └── schema changed by → migrations
    ├── storage ............... buckets → objects, policies    → storage.md
    ├── variables ............. env vars / secrets             → variables.md
    ├── frontends ............. deployed sites, custom domains → frontends.md
    └── declarative config .... one YAML syncing the above     → ../project-configuration.md
```

Cross-cutting concerns:

- **Authentication** gates all cloud access — see [authentication.md](authentication.md).
- **Declarative configuration** applies settings across every element from one
  manifest — see [project-configuration.md](project-configuration.md).

## Local vs. cloud target

The CLI can operate against two targets:

- **Local** development environment (`volcano start` / `stop` / `restart` /
  `status` / `reset`), used by the top-level resource commands when it is
  running.
- **Cloud** project — forced with the `cloud` prefix (e.g. `volcano cloud
  functions list`).

Top-level resource commands (`volcano functions …`, `volcano databases …`, etc.)
act on your **active context**: the local environment when running, otherwise
the cloud project.

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

## Element → CLI ability at a glance

| Element | CLI can… | Commands | Details |
|---|---|---|---|
| Account / auth | sign up, log in/out | `signup`, `login`, `logout` | [authentication.md](authentication.md) |
| Project | create, list, get, delete, select, get anon keys | `projects …`, `use` | below |
| Functions | deploy, invoke, inspect, schedule, alias | `functions …` | [functions.md](functions.md) |
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
volcano projects keys              # anon (publishable) API keys for the browser/SDK
volcano projects delete my-app     # delete
```

`VOLCANO_PROJECT_ID` overrides the active project for a single invocation
(useful in CI).

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
