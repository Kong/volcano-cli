---
title: "LavaFlow"
description: "Turn LavaFlow on from the CLI so Volcano diagnoses your project's runtime errors and opens draft pull requests that fix them."
---

## What it is

LavaFlow reads your project's runtime errors, groups them into issues,
diagnoses each one, and opens a draft pull request with a fix in the project's
connected repository. Turning it on is the whole setup: there is nothing to
choose and no key to bring.

Once it is on, LavaFlow diagnoses new and reopened issues on its own and opens
draft fix pull requests without a request. You review and merge them like any
other pull request.

LavaFlow is in preview and needs a paid plan. LavaFlow is a usage-billed
add-on, priced per issue and per fix pull request. Prices are announced before
billing starts.

## How it relates

- Belongs to a **project**. It reads the runtime errors of the project's
  functions and frontends.
- Opens pull requests in the repository the **Git connection** names. Without
  one, LavaFlow diagnoses issues and opens no pull requests. See
  [git.md](git.md).
- Declared in the **project configuration** under `lavaflow:`. See
  [project-configuration.md](project-configuration.md).
- Cloud only. Local development has no GitHub App for LavaFlow to open pull
  requests with, so `volcano lavaflow` refuses and points to
  `volcano cloud lavaflow`.

See [LavaFlow](/platform/guides/lavaflow) for how runs work and their limits.

## CLI operations

| Operation | Command |
|---|---|
| Show whether LavaFlow is on | `volcano cloud lavaflow status [--json]` |
| Turn LavaFlow on | `volcano cloud lavaflow enable [--json]` |
| Turn LavaFlow off | `volcano cloud lavaflow disable [--json]` |
| List runs | `volcano cloud lavaflow runs list [--limit n] [--cursor c] [--json]` |
| Show a run and its diagnosis | `volcano cloud lavaflow runs get <run-id> [--json]` |
| Diagnose an issue again | `volcano cloud lavaflow runs start --issue <issue-id> [--json]` |

Turning LavaFlow on or off and starting a run need your login: a project access
token is refused, and so is a `config deploy` with one that changes the
`lavaflow` section. A project access token can read the status and runs.

## Turn it on

```bash
volcano git connect
volcano cloud lavaflow enable
```

```text
✓ LavaFlow is on
Note: LavaFlow diagnoses new and reopened issues on its own and opens draft pull requests that fix them in the project's connected repository.
Note: LavaFlow is a usage-billed add-on, priced per issue and per fix pull request. Prices are announced before billing starts.
```

A project whose plan does not allow LavaFlow is refused, and the command exits
non-zero. Turn it off again with `volcano cloud lavaflow disable`. Runs that
have not finished are canceled; past runs and their pull requests stay.
Turning LavaFlow off works even when the project's plan cannot be checked; then
`max_stage` and `auto_trigger` go back to their defaults, and the command says
so.

## Check the status

```bash
volcano cloud lavaflow status
```

```text
LavaFlow: on
Plan: allows LavaFlow
Repository: acme/shop

LavaFlow is a usage-billed add-on, priced per issue and per fix pull request. Prices are announced before billing starts.
```

When no repository is connected, the status says so and names
`volcano git connect`. When the project configuration narrows LavaFlow, with
`max_stage: diagnose` or `auto_trigger: false`, the status and `enable` say
that too. With `--json`, `repository_state` is `connected`, `not_connected`, or
`unknown` when the connection could not be read.

## Follow the runs

A run diagnoses one issue. List the newest first:

```bash
volcano cloud lavaflow runs list
```

```text
ID                                    Issue                                 Trigger     Status      Result                      Created
--------------------------------------------------------------------------------------------------------------------------------------------
9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c  TypeError: cart is undefined          new issue   succeeded   PR #42 open                 2h ago
```

`runs get` shows a run's diagnosis: a summary, the root cause, the suggested
fix, the evidence it found, and the pull request it opened. When it opened
none, it says why, such as a code change not fixing the issue. Add `--json` for
the full run, including the proposed edits.

```bash
volcano cloud lavaflow runs get 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
```

A queued run can wait. The status says `waiting` when Volcano has paused
LavaFlow runs; the run resumes on its own.

To diagnose an issue again, for example after a run failed, start a run with
the issue ID from an earlier run in `runs list --json`, or from the
[Issues API](/platform/guides/issues):

```bash
volcano cloud lavaflow runs start --issue 3f5a8b1c-2d4e-4f6a-8b9c-0d1e2f3a4b5c
```

An issue has at most one queued or running run, and a project starts at most
25 runs in any 24 hours.

## Declare it in the project configuration

```yaml
version: 1
lavaflow:
  enabled: true
```

`enabled` is all most projects need. Two more fields narrow what LavaFlow
does. `max_stage: diagnose` stops each run at the diagnosis and opens no pull
request; the default, `pull_request`, opens one. `auto_trigger: false` starts
runs only with `runs start`; the default, `true`, starts one for every new or
reopened issue. An omitted field keeps its current value. `enable` and
`disable` change only `enabled`, so they keep what the manifest set.
