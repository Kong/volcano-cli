---
title: "LavaFlow"
description: "Turn LavaFlow on from the CLI so Volcano diagnoses your project's runtime errors and opens pull requests that fix them, then follow what it did."
---

## What it is

LavaFlow uses AI to diagnose this project's errors from their logs and
deployed source, and opens fix pull requests in your connected repository.
It covers every function, frontend and durable function in the project.
Turning it on is the whole setup: there is nothing to install and no key to
bring. You review and merge its pull requests like any other.

Where LavaFlow isn't available, every `volcano cloud lavaflow` command says
why and exits with code 4:

- While LavaFlow is open only to some accounts, every command on any other
  account says "LavaFlow isn't available to your account yet".
- On a Volcano API that has no LavaFlow, every command says "LavaFlow isn't
  available on this Volcano API".

## How it relates

- Belongs to a **project**. It reads the runtime errors of the project's
  functions, frontends and durable functions.
- Opens pull requests in the repository the **Git connection** names. Without
  one, LavaFlow diagnoses issues and opens no pull requests. See
  [git.md](git.md).
- Fixes code it can read at the commit that built it. See
  [Source commit](deploy.md#source-commit).
- Cloud only. LavaFlow runs on Volcano's cloud service, which local
  development doesn't run, so `volcano lavaflow` refuses and points to
  `volcano cloud lavaflow`.

## CLI operations

| Operation | Command |
|---|---|
| Show whether LavaFlow is on | `volcano cloud lavaflow status [--json]` |
| Turn LavaFlow on | `volcano cloud lavaflow enable [--json]` |
| Turn LavaFlow off | `volcano cloud lavaflow disable [--json]` |
| Show the settings in full | `volcano cloud lavaflow settings [--json]` |
| Change the settings | `volcano cloud lavaflow settings set [--autonomy a] [--custom-context t \| --custom-context-file f] [--system-prompt t \| --system-prompt-file f] [--json]` |
| Show issues and fix pull requests per day | `volcano cloud lavaflow stats [--days n] [--json]` |
| Show what LavaFlow did for each resource | `volcano cloud lavaflow resources [--days n] [--json]` |
| List runs | `volcano cloud lavaflow list [--status s] [--resource id] [--limit n] [--cursor c] [--json]` |
| Show a run | `volcano cloud lavaflow show <run-id> [--json]` |

Every command takes `--json` for the API's own fields and values.

Turning LavaFlow on or off and changing its settings need a person's session:
a project access token is refused before anything is sent, and the command
exits with code 5. Sign in with `volcano login` instead; if `VOLCANO_TOKEN`
holds a project access token, unset it first. A project access token can run
every other command.

## Turn it on

```bash
volcano git connect
volcano cloud lavaflow enable
```

```text
✓ LavaFlow is on
LavaFlow: on
Autonomy: open fix pull requests
Custom context: not set
System prompt: not set
```

`disable` turns it off. No new run starts; past runs and their pull requests
stay readable.

## Check the status

```bash
volcano cloud lavaflow status
```

```text
LavaFlow: on
Autonomy: open fix pull requests
Repository: acme/shop
Custom context: not set
System prompt: not set
Updated: 2026-10-30 12:00
```

While LavaFlow is off, the status says what it does and how to turn it on.
When it is set to open pull requests and no repository is connected, the
status says to run `volcano git connect`. With `--json`, `repository_state`
is `connected`, `not_connected`, or `unknown` when the connection could not
be read.

## Choose how far it goes

```bash
volcano cloud lavaflow settings set --autonomy diagnose_only
```

`--autonomy` takes one of two values:

- `open_pr`: LavaFlow diagnoses each issue and opens a fix pull request for
  review.
- `diagnose_only`: LavaFlow diagnoses each issue and leaves any fix to a
  person.

Two more settings shape every run:

- `--custom-context`: what LavaFlow should know about the project, such as
  which code is legacy or how it is deployed.
- `--system-prompt`: instructions added to LavaFlow's own, such as how small
  a fix should be.

Each takes up to 4,000 characters, from the flag or a file
(`--custom-context-file`, `--system-prompt-file`). An empty value clears it.
A setting you don't pass keeps its value. `settings` shows both in full.

## See what it did

`stats` counts the issues LavaFlow took on and the fix pull requests it opened,
per UTC day, today included. `--days` takes 1 to 90 and defaults to 30.

```bash
volcano cloud lavaflow stats --days 7
```

`resources` lists every function, frontend and durable function: when
LavaFlow last analyzed it, and the issues and fix pull requests in the window.
A resource whose issues suddenly jumped is flagged `issue rate jumped`, with
the reason: at least 3 issues in the last 24 hours, and at least 3 times its
daily average over the 29 days before.

```bash
volcano cloud lavaflow resources
```

```text
Type              Name                            Last analyzed     Issues  Fix PRs
----------------------------------------------------------------------------------------------------
function          checkout                        2026-10-30 09:00       6        3  issue rate jumped
durable function  nightly-report                  never                  0        0
```

## Follow the runs

A run works on one issue. `list` shows the newest first, with the resource,
the issue's title, and the fix pull request. Narrow it with `--status`
(`queued`, `diagnosing`, `fixing`, `reviewing`, `pr_open`, `completed`,
`failed` or `cancelled`) or `--resource` with an ID from
`resources --json`. When there are more, the list ends with the command for
the next page.

```bash
volcano cloud lavaflow list --status failed
```

`show` prints a run's diagnosis and root cause, the evidence and log lines it
read, its review, the fix pull request, and the diff. A run that failed says
why. Reading a run needs the permission to read the project's logs.

```bash
volcano cloud lavaflow show 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
```

```text
Run: 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
Status: PR open
Resource: function checkout
Title: TypeError: Cannot read properties of undefined (reading <str>)
Started: 2026-10-30 09:00
Pull request: #42 (open) https://github.com/acme/shop/pull/42
Review: passed after 2 round(s)

Diagnosis
  Checkout reads the cart before it loads.
```

An issue's title is a template of its error message, with values replaced by
placeholders such as `<str>`.
