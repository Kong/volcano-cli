---
title: "LavaFlow"
description: "Turn LavaFlow on from the CLI so Volcano diagnoses your project's runtime errors and opens pull requests that fix them, then follow what it did."
---

## What it is

LavaFlow uses AI to diagnose this project's errors from their logs and
deployed source, and opens fix pull requests in your connected repository.
It covers every function, frontend and durable function in the project.
Turning it on is the whole setup: there is nothing to install and no key to
bring. You review and merge its pull requests like any other, unless you
choose autonomy `merge`: then LavaFlow can merge them, and a fix it holds or
stops is yours to review and merge on GitHub.

Where LavaFlow isn't available, every `volcano cloud lavaflow` command says
why:

- When LavaFlow isn't open to the project or its owner yet, every command
  says "LavaFlow isn't available for this project yet" and exits with code 4.
  Volcano checks this before the owner's plan.
- When LavaFlow is open to the project but the owner's plan doesn't include
  it, every command says "LavaFlow needs a plan that includes it, and the
  project owner's plan doesn't" and exits with code 5.
- On a Volcano API that has no LavaFlow, every command says "LavaFlow isn't
  available on this Volcano API" and exits with code 4.

A project that already has LavaFlow records, such as saved settings or past
runs, keeps them readable while LavaFlow isn't available for it:
`status`, `settings`, `stats`, `resources`, `list` and `show` still work, and
`disable` still turns LavaFlow off. `enable` says "LavaFlow can't be turned on
for this project: it isn't open to the owner's account, or the owner's plan
doesn't include it" before it asks anything, and exits with code 4. It says
the same when the owner's plan couldn't be read.
`settings set` exits with the plan's code 5 or code 4, as above.

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
| Turn LavaFlow on | `volcano cloud lavaflow enable [--yes] [--json]` |
| Turn LavaFlow off | `volcano cloud lavaflow disable [--json]` |
| Show the settings in full | `volcano cloud lavaflow settings [--json]` |
| Change the settings | `volcano cloud lavaflow settings set [--autonomy a] [--human-review-path p]... [--clear-human-review-paths] [--custom-context t \| --custom-context-file f] [--system-prompt t \| --system-prompt-file f] [--yes] [--json]` |
| Show issues and fix pull requests per day | `volcano cloud lavaflow stats [--days n] [--json]` |
| Show what LavaFlow did for each resource | `volcano cloud lavaflow resources [--days n] [--json]` |
| List runs | `volcano cloud lavaflow list [--status s] [--resource id] [--limit n] [--cursor c] [--json]` |
| Show a run | `volcano cloud lavaflow show <run-id> [--json]` |

Every command takes `--json` for the API's own fields and values.

Text from a run, such as its diagnosis, log lines, diff and title, prints as
plain text. A control character in it, such as a color code or a carriage
return in a log line, prints as a visible escape such as `\x1b`, and with
`--json` it stays escaped inside the JSON string.

LavaFlow belongs to the project's owner. For anyone else, every command says
"project not found", as for a project that doesn't exist, and exits with
code 4. A project access token can run the read commands for its own
project, but only the owner's own session can turn LavaFlow on or off and
change its settings: `enable`, `disable` and `settings set` refuse a project
access token before they ask or send anything, with code 5. Sign in with
`volcano login` instead, and if `VOLCANO_TOKEN` holds a project access token,
unset it first.

When the owner's plan can't be read, a command says "LavaFlow couldn't read
the project owner's plan; try again later" and exits with code 5. A change
that `enable`, `disable` or `settings set` sends can also be refused for a
read-only access token or account; the API doesn't tell those apart from a
plan it couldn't read, so the command names all three, adds what Volcano
said, and exits with code 5.

## Turn it on

```bash
volcano git connect
volcano cloud lavaflow enable
```

```text
LavaFlow uses AI to diagnose this project's errors from their logs and deployed source, and opens fix pull requests in your connected repository.
LavaFlow sends this project's logs and source code to an AI service that Volcano operates.
Turn LavaFlow on? Type 'y' or 'yes' to confirm: y
✓ LavaFlow is on
LavaFlow: on
Autonomy: open fix pull requests
Human review paths: none
Custom context: not set
System prompt: not set
```

`enable` says what LavaFlow does and where it sends the project's data, then
asks before it turns LavaFlow on. `--yes` skips the question, not those
sentences, which go to stderr so `--json` output stays JSON. Without `--yes`, `enable` needs a
terminal to answer: in a script or CI job it exits non-zero and changes
nothing.

`disable` turns it off. No new run starts; past runs and their pull requests
stay readable.

## Check the status

```bash
volcano cloud lavaflow status
```

```text
LavaFlow: on
Autonomy: open fix pull requests
Merging: available
Repository: acme/shop
Custom context: not set
System prompt: not set
Updated: 2026-10-30 12:00
```

While LavaFlow is off, the status says what it does and how to turn it on.
When you use a project access token, which can read but not change the
settings, the status says so instead.
When it is set to open pull requests and no repository is connected, the
status says to run `volcano git connect`. When LavaFlow isn't available for
a project that has LavaFlow records, the status notes that it can't be turned
on or changed, and that the owner can still turn it off.

`Merging` says whether you can choose autonomy `merge` for the project.
While it reads `not available for this project yet`,
`settings set --autonomy merge` is refused. The line is left out when the
project's capabilities could not be read or don't list merging.

With `--json`, `repository_state` is `connected`, `not_connected`, or
`unknown` when the connection could not be read. `available` is `true` when
LavaFlow can be turned on and changed for the project and `false` when it
can't; it is left out when the project's capabilities could not be read.
`merge_available` is `true` when LavaFlow can merge its fixes for the project
and `false` when it can't; it is left out when the `Merging` line is.

## Choose how far it goes

```bash
volcano cloud lavaflow settings set --autonomy diagnose_only
```

`--autonomy` takes one of three values:

- `open_pr`: LavaFlow diagnoses each issue and opens a fix pull request for
  you to review and merge.
- `diagnose_only`: LavaFlow diagnoses each issue and leaves any fix to a
  person.
- `merge`: LavaFlow can also merge its fix pull requests. See
  [Merge fixes](#merge-fixes).

Two more settings are saved for LavaFlow's runs, which don't use them yet:

- `--custom-context`: what LavaFlow should know about the project, such as
  which code is legacy or how it is deployed.
- `--system-prompt`: instructions added to LavaFlow's own, such as how small
  a fix should be.

Each takes up to 4,000 characters, from the flag or a file
(`--custom-context-file`, `--system-prompt-file`). An empty value clears it.
A setting you don't pass keeps its value. `settings` shows both in full.

### Merge fixes

At autonomy `merge`, LavaFlow can merge its fix pull requests. A fix it holds
or stops is yours to review and merge on GitHub.

```bash
volcano cloud lavaflow settings set --autonomy merge
```

Turning on merge lets LavaFlow's fixes, including those from runs it starts
on its own, run your CI with the secrets your pull request workflows get.

`--autonomy merge` prints that and asks first. `--yes` skips the question but
still prints it, on stderr, so `--json` output stays JSON. Without a terminal
and without `--yes`, it changes nothing and exits with a non-zero code.

Only the project's owner can turn merging on, signed in with `volcano login`
while that sign-in lasts, or with a platform token that has an expiry. Volcano
refuses a platform token that never expires, or a sign-in that ended, and the
command says why in Volcano's words and exits with code 5. While
`VOLCANO_TOKEN` is set, its token is used instead of a `volcano login`
sign-in. A project access token is refused before anything is sent, also with
code 5. Merging applies to the
repository the project is connected to when you turn it on: once the project
is connected to another, autonomy reads `open_pr` until you turn merge on
again. Merging is only for a project where
it's available, which `status` shows; elsewhere `--autonomy merge` says
"LavaFlow merging isn't available for this project yet" and exits with
code 1. When `status` shows merging isn't available while LavaFlow is, it
says so before it warns or asks. A project already at `merge` can still save
`--autonomy merge` again. When the owner's GitHub account can't push to the
project's repository, it says so, adds what Volcano said, points to
[connecting GitHub](/platform/projects/git-deploy#1-connect-github), and exits
with code 5.

`--human-review-path` names a path whose changes always hold a fix for a
person, such as `db/migrations/**`. Repeat it for each path, up to 50, each 1
to 200 characters. It replaces the saved paths; `--clear-human-review-paths`
removes them all. Volcano's own held paths, such as deploy configuration and
authentication code, always hold a fix too. `settings` prints the paths. Like
`--autonomy merge`, both flags need merging to be available for the project;
elsewhere they say "LavaFlow merging isn't available for this project yet"
and exit with code 1.

```bash
volcano cloud lavaflow settings set --human-review-path 'infra/**' --human-review-path 'db/migrations/**'
```

`show` prints `Merge:` with where the merge is: `waiting for CI`,
`queued to merge`, `merged`, `held` or `stopped`. While merging is paused for
the project, a waiting fix says `paused while merging is off`. `held` and
`stopped` are final: LavaFlow doesn't touch the pull request again, and it's
yours to review and merge on GitHub. A held fix lists each reason in Volcano's
words, with the paths it names; a stopped fix gives its reason, such as
`checks failed`. While the pull request is open, `show` ends with a line
saying it's yours, and in `list` the run shows `Held: yours to merge` or
`Stopped: yours to merge` as its status. Once someone merges or closes the
pull request, `show` keeps the `Merge:` line and its reasons, and `list`
shows the run's own status.

```text
Merge: held
  - It changes a path in human_review_paths. (db/migrations/0042_orders.sql)
The pull request is now yours to review and merge on GitHub.
```

## See what it did

`stats` counts the issues LavaFlow took on and the fix pull requests it opened,
per UTC day, today included. Merges aren't counted. `--days` takes 1 to 90 and
defaults to 30.

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
the run's title, and the fix pull request. A run's title is the title of the
fix it proposed, or its diagnosis summary; until LavaFlow has either, the
title shows `-`. Narrow it with
`--status` (`queued`, `diagnosing`, `fixing`, `reviewing`, `pr_open`,
`completed`, `failed` or `cancelled`) or `--resource` with an ID from
`resources --json`. When there are more, the
list ends with the command for the next page. With `--json`, pass
`next_cursor` to `--cursor`, with the same `--limit`, `--status` and
`--resource`.

```bash
volcano cloud lavaflow list --status failed
```

A running run's status says which stage it is in: "Running: finding the root
cause" (`diagnosing`), "Running: writing the fix" (`fixing`) or "Running:
reviewing the fix" (`reviewing`). An open pull request shows as "PR open". A
status the CLI doesn't know prints as the API gives it.

`show` prints a run's diagnosis and root cause, the evidence and log lines it
read, its review, the fix pull request and when it opened, where its merge is
(see [Merge fixes](#merge-fixes)), and the diff when the run has one. A pull
request opened before LavaFlow recorded the time has no open time. A run that
failed says why.

```bash
volcano cloud lavaflow show 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
```

```text
Run: 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
Status: PR open
Resource: function checkout
Title: Load the cart before checkout reads it
Issue: 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e
Started: 2026-10-30 09:00
Pull request: #42 (open) https://github.com/acme/shop/pull/42
Pull request opened: 2026-10-30 09:03
Review: passed after 2 round(s)

Diagnosis
  Checkout reads the cart before it loads.
```
