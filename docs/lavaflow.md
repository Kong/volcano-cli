---
title: "LavaFlow"
description: "Turn LavaFlow on from the CLI so Volcano diagnoses your project's runtime errors and opens pull requests that fix them, then follow what it did."
---

## What it is

LavaFlow uses AI to diagnose this project's errors from their logs and
deployed source, and opens fix pull requests in your connected repository.
It covers every function, frontend and durable function in the project.
Turning it on is the whole setup: there is nothing to install and no key to
bring. At autonomy `merge`, where merging is available for the project,
LavaFlow also merges a reviewed fix, by default only after a person approves
it. Otherwise LavaFlow never merges: you review and merge its pull requests
like any other.

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
| Change the settings | `volcano cloud lavaflow settings set [--autonomy a] [--merge-mode m] [--human-review-path p]... [--clear-human-review-paths] [--allow-merge-without-checks b] [--allow-ci-before-approval b] [--custom-context t \| --custom-context-file f] [--system-prompt t \| --system-prompt-file f] [--json]` |
| Show issues and fix pull requests per day | `volcano cloud lavaflow stats [--days n] [--json]` |
| Show what LavaFlow did for each resource | `volcano cloud lavaflow resources [--days n] [--json]` |
| List runs | `volcano cloud lavaflow list [--status s] [--resource id] [--limit n] [--cursor c] [--json]` |
| Show a run | `volcano cloud lavaflow show <run-id> [--json]` |
| Approve the merge a run waits for | `volcano cloud lavaflow approve <run-id> [--yes] [--json]` |

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

With `--json`, `repository_state` is `connected`, `not_connected`, or
`unknown` when the connection could not be read. `available` is `true` when
LavaFlow can be turned on and changed for the project and `false` when it
can't; it is left out when the project's capabilities could not be read.

## Choose how far it goes

```bash
volcano cloud lavaflow settings set --autonomy diagnose_only
```

`--autonomy` takes one of three values:

- `open_pr`: LavaFlow diagnoses each issue and opens a fix pull request for
  you to review and merge.
- `diagnose_only`: LavaFlow diagnoses each issue and leaves any fix to a
  person.
- `merge`: LavaFlow also merges each reviewed fix, as the merge mode allows.
  It's only for a project where merging is available; elsewhere it says
  "LavaFlow merging isn't available for this project yet".

## Choose when it merges

At autonomy `merge`, `--merge-mode` decides when LavaFlow merges a reviewed
fix:

- `human`, the default: A person approves every merge.
- `auto`: LavaFlow merges a fix that passes review and its required checks, unless the project's merge instructions or a platform hold say a person must approve it.

In every mode, anyone with write access to the repository can approve with an
approving GitHub review at the fix's current head, as well as in Volcano. A
new commit voids an approval, and bot and App reviews don't count. Reviewer
concerns block merging.

```bash
volcano cloud lavaflow settings set --autonomy merge --merge-mode human
```

`settings` and `status` show the mode as `Merging:`. The mode is kept at any
autonomy, and below `merge` the line says merging is off.

### Merge instructions

`--merge-instructions` says, in your own words, which fixes need a person in
`auto` mode, such as "Billing changes and anything irreversible need a
person." LavaFlow judges each fix against them, and they only add holds, never
release one. They take up to
4,000 characters, from the flag or a file (`--merge-instructions-file`, where
`-` reads stdin). An empty value goes back to Volcano's default instructions.
`settings` prints the instructions in full, or Volcano's default ones when
none are set.

```bash
volcano cloud lavaflow settings set --merge-instructions-file merge-instructions.md
```

The merge instructions guide LavaFlow's judgment and aren't a security
boundary. Use `--human-review-path` for anything that must always wait for a
person.

The rest of the merge policy:

- `--human-review-path`: a path whose changes always wait for a person, in
  any mode. Repeat it for each path, up to 50, each 1 to 200 characters. It
  replaces the saved paths; `--clear-human-review-paths` removes them all.
- `--allow-merge-without-checks true|false`: whether LavaFlow may merge where
  the repository requires no checks. It's `false` unless you change it.
- `--allow-ci-before-approval true|false`: whether CI runs before a person
  approves. Only `auto` allows `true`.

```bash
volcano cloud lavaflow settings set --human-review-path 'infra/**' --human-review-path 'migrations/**'
```

Each flag changes only its own field. `settings` prints the whole policy, and
`settings --json` has it under `merge_policy`.

## Other settings

Two more settings are saved for LavaFlow's runs, which don't use them yet:

- `--custom-context`: what LavaFlow should know about the project, such as
  which code is legacy or how it is deployed.
- `--system-prompt`: instructions added to LavaFlow's own, such as how small
  a fix should be.

Each takes up to 4,000 characters, from the flag or a file
(`--custom-context-file`, `--system-prompt-file`). An empty value clears it.
A setting you don't pass keeps its value. `settings` shows both in full.

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
read, its review, the fix pull request and when it opened, and the diff when
the run has one. A pull request opened before LavaFlow recorded the time has
no open time. A run that failed says why.

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

## Approve a merge

When a run's merge waits for a person, `show` says so and why, then names the
command that approves:

```text
Merge: waiting for your approval
  - LavaFlow started this fix from your logs on its own. Approving runs CI and merges it.
Note: Approve the merge with 'volcano cloud lavaflow approve 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c', or close the pull request to stop it.
```

Each reason prints as one line, worded by Volcano, followed by the paths it is
about, if any. A reason without wording prints as "Waiting for your approval".
Once approved, `show` says which approval applies, and `Merge:` says where the
merge is: `waiting for CI`, `queued to merge`, `merged` or `stopped`, with the
reason it stopped. `Held paths approved:` lists the held paths the fix changes
that a person approved it past.

```text
Merge: waiting for CI
Approved on GitHub by @octo-reviewer at 2026-10-30 09:05
```

In `list`, a run whose merge waits for a person shows
`Waiting for your approval` as its status.

```bash
volcano cloud lavaflow approve 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
```

`approve` shows the run, its pull request and why the merge waits, then asks
before it approves; `--yes` skips the question. Approving runs CI on the fix,
and LavaFlow merges it once the required checks pass. To stop a merge
instead, close the pull request. A run whose merge doesn't wait for a person
is refused before anything is sent. If the pull request changed after
`approve` read the run, nothing is approved: check the run again with `show`,
then approve it.

Anyone with write access to the repository can also approve with an
approving GitHub review at the fix's current head. A new commit voids it, and
bot and App reviews don't count.

In Volcano, only the project's owner approves, from their own session. A
project access token is refused before anything is read, with code 5: sign in
with `volcano login` instead, and if `VOLCANO_TOKEN` holds a project access
token, unset it first. Someone other than the owner is refused with code 5
too. When Volcano can't check the owner's GitHub access right then, nothing is
approved, and `approve` says when to try again.
LavaFlow merges with the owner's GitHub account, so when that account can't
push to the repository, `approve` says so and points to
[connecting GitHub](/platform/projects/git-deploy#1-connect-github). Where
merging isn't available for the project, `approve` says "LavaFlow merging
isn't available for this project yet".
