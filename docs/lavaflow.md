---
title: "LavaFlow"
description: "Turn LavaFlow on from the CLI so Volcano diagnoses your project's runtime errors and opens pull requests that fix them."
---

## What it is

LavaFlow uses AI to diagnose this project's errors from their logs and
deployed source, and opens fix pull requests in your connected repository.
Turning it on is the whole setup: there is nothing to choose and no key to
bring.

By default, runs start without a request once LavaFlow is on. A project
configuration with `auto_trigger: false` keeps them to `runs start` instead
(see "Declare it in the project configuration" below). You review and merge
its pull requests like any other.

LavaFlow is in preview. During the preview, it needs the SUPERAGENT plan.

Where LavaFlow isn't available, a `volcano cloud lavaflow` command that needs
it says "LavaFlow isn't available on this Volcano API" and exits with code 4:

- On a Volcano API that has no LavaFlow, every command says so.
- Where Volcano has LavaFlow off, `enable` and `runs start` say so. A project
  that saved LavaFlow settings can still read its status and turn LavaFlow
  off; for a project that never saved them, `status` and `disable` say so
  too. `runs list` and `runs get` still show past runs.

## How it relates

- Belongs to a **project**. It reads the runtime errors of the project's
  functions and frontends.
- Opens pull requests in the repository the **Git connection** names. Without
  one, LavaFlow diagnoses issues and opens no pull requests. See
  [git.md](git.md).
- Fixes code it can read at the commit that built it: code that a push to the
  connected repository deployed. For any other deployment, LavaFlow diagnoses
  the issue and opens no pull request.
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
`lavaflow` section. With a project access token, `enable` says so and sends
nothing. A project access token can read the status and runs.

## Turn it on

```bash
volcano git connect
volcano cloud lavaflow enable
```

```text
✓ LavaFlow is on
Note: LavaFlow uses AI to diagnose this project's errors from their logs and deployed source, and opens fix pull requests in your connected repository.
Note: LavaFlow diagnoses the next issue that happens a second time or comes back after you resolve it, and opens a fix pull request in acme/shop.
Note: LavaFlow is in preview. During the preview, it needs the SUPERAGENT plan.
```

`enable` says what LavaFlow does, then in one line what it does next, naming
the repository its fix pull request goes to. With no repository connected, it
says LavaFlow stops at the diagnosis until you connect one, and that
connecting one pauses LavaFlow until you turn it on again. It names
`volcano git connect`, then `volcano cloud lavaflow enable`.

A project whose plan does not allow LavaFlow is refused, and the command exits
non-zero. A project that had LavaFlow on before its plan changed stays on;
`enable` then succeeds and says no run starts until the plan allows LavaFlow
again. Turn it off again with `volcano cloud lavaflow disable`. Runs that
have not finished are canceled; past runs and their pull requests stay.
Where LavaFlow is not available, turning it on fails with "LavaFlow isn't
available on this Volcano API". Turning it off still works for a project that has saved LavaFlow
settings. A project that never saved them already has LavaFlow off, and
`disable` fails the same way.

## Check the status

```bash
volcano cloud lavaflow status
```

```text
LavaFlow: on, opening fix pull requests in acme/shop
Plan: allows LavaFlow
Repository: acme/shop

LavaFlow is in preview. During the preview, it needs the SUPERAGENT plan.
```

The first row says the state at a glance: `off`, or what LavaFlow does while
it is on, such as `on, diagnosing only until a repository is connected`. The
status says when no repository is connected. When LavaFlow is on and set
to open pull requests, it also says to connect a repository and then turn
LavaFlow on again, naming `volcano git connect`, then
`volcano cloud lavaflow enable`, because connecting a repository pauses
LavaFlow until it is turned on again. When the project configuration narrows LavaFlow, with
`max_stage: diagnose` or `auto_trigger: false`, the status and `enable` say
that too, and the status says it while LavaFlow is off, because those settings
apply when it is turned on. With `--json`, `repository_state` is `connected`,
`not_connected`, or `unknown` when the connection could not be read.

## When the project's repository changes

LavaFlow works in the repository the project was connected to when a person
turned it on. A rename or a transfer on GitHub keeps it the same repository.
When the project is connected to a different repository, or to none, or a
repository is connected after LavaFlow was turned on with none, LavaFlow
pauses: no run starts until someone turns LavaFlow on again, and a run already
queued reads no source and opens no pull request. The status says so:

```text
LavaFlow: on, but paused until it is turned on again
Plan: allows LavaFlow
Repository: acme/new

Warning: The project's repository changed from acme/old since LavaFlow was turned on, so no run starts until someone turns LavaFlow on again.
LavaFlow is in preview. During the preview, it needs the SUPERAGENT plan.
Resume LavaFlow with acme/new: volcano cloud lavaflow enable
```

`volcano cloud lavaflow enable`, run by a person, resumes LavaFlow in the
repository the project is connected to now. With no repository connected, it
resumes LavaFlow without one, and the status also names `volcano git connect`.
Until then, `runs start` prints why it was refused, with the same command, and
exits non-zero. A run that opened no pull request because the repository
changed says to turn LavaFlow on again before starting a new run. With
`--json`, the status carries `repository_changed` and `enabled_repository`, the
repository LavaFlow was turned on for.

## Follow the runs

A run diagnoses one issue. List the newest first:

```bash
volcano cloud lavaflow runs list
```

```text
ID                                    Issue                                 Trigger     Status      Result                      Created
--------------------------------------------------------------------------------------------------------------------------------------------
9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c  TypeError: Cannot read properties...  new issue   succeeded   PR #42 open                 2h ago
```

An issue's title is a template of its error message, with values replaced by
placeholders such as `<str>`. The list shortens long titles; `runs get` shows
the whole title.

The result says why a run stopped or opened no pull request, such as
`no PR: low confidence` or `timed out`. When a run on the page ended without a
fix, the list ends with the `runs get` command that says what to do about it.

`runs get` shows a run's diagnosis: a summary, the root cause, the suggested
fix, the evidence it found, and the pull request it opened. When it opened
none, it says why, such as a code change not fixing the issue, LavaFlow not
being able to read the deployed source, or `max_stage: diagnose`. A run that
stopped or opened no pull request ends with `Next:` and what to do about it,
such as the `runs start` command that diagnoses the issue again. Add `--json`
for the run as JSON, including the proposed edits.

```bash
volcano cloud lavaflow runs get 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
```

```text
ID: 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
Issue: TypeError: Cannot read properties of undefined (reading <str>) (3f5a8b1c-2d4e-4f6a-8b9c-0d1e2f3a4b5c)
Trigger: new issue
Status: succeeded
...
Pull request: https://github.com/acme/shop/pull/42 (open)
```

A queued run can be paused. Its result says `paused` while LavaFlow is
unavailable or Volcano has paused LavaFlow's runs, and the run carries on by
itself when LavaFlow resumes, without using an attempt. A run paused after it
wrote a fix keeps its diagnosis and opens its pull request then. A queued run
whose last attempt failed says `retrying`.

To diagnose an issue again, for example after a run failed, start a run with
the issue ID (`trigger.issue.id`) from an earlier run in `runs list --json`, or
from the
[Issues API](/platform/guides/issues):

```bash
volcano cloud lavaflow runs start --issue 3f5a8b1c-2d4e-4f6a-8b9c-0d1e2f3a4b5c
```

An issue has at most one queued or running run. LavaFlow also limits how many
runs an issue and a project start in 24 hours. At a limit, `runs start` prints
the reason and when to try again, and exits non-zero; it never retries on its
own.

## Declare it in the project configuration

```yaml
version: 1
lavaflow:
  enabled: true
```

`enabled` is all most projects need. Two more fields narrow what LavaFlow
does. `max_stage: diagnose` stops each run at the diagnosis and opens no pull
request; the default, `pull_request`, opens one. `auto_trigger: false` starts
runs only with `runs start`; the default, `true`, starts one when an issue
happens a second time or comes back after you resolve it, within LavaFlow's run
limits. An omitted field keeps its current value. `enable` and
`disable` change only `enabled`, so they keep what the manifest set.
