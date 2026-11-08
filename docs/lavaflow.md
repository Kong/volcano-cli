---
title: "LavaFlow"
description: "Turn LavaFlow on from the CLI so Volcano diagnoses your project's runtime errors and opens pull requests that fix them, then follow what it did."
---

## What it is

LavaFlow uses AI to diagnose this project's errors from their logs and
deployed source, and opens fix pull requests in your connected repository.
It covers every function, frontend and durable function in the project.
With a connected repository, turning it on is the whole setup: there is
nothing to install and no key to bring. You review and merge its pull requests like any other, unless you
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
| Change the settings | `volcano cloud lavaflow settings set [--autonomy a] [--human-review-path p]... [--clear-human-review-paths] [--custom-context t \| --custom-context-file f] [--system-prompt t \| --system-prompt-file f] [--allow-ungated-workflows true\|false] [--yes] [--json]` |
| Show issues and fix pull requests per day | `volcano cloud lavaflow stats [--days n] [--json]` |
| Show what LavaFlow did for each resource | `volcano cloud lavaflow resources [--days n] [--json]` |
| List runs | `volcano cloud lavaflow list [--status s] [--resource id] [--limit n] [--cursor c] [--json]` |
| Show a run | `volcano cloud lavaflow show <run-id> [--json]` |
| Start a run on an issue | `volcano cloud lavaflow runs start <issue-id> [--json]` |

Every command takes `--json` for the API's own fields and values.

Text from a run, such as its diagnosis, evidence and title, prints as plain
text. A control character in it, such as a color code or a carriage return
copied from a log, prints as a visible escape such as `\x1b`, and with
`--json` it stays escaped inside the JSON string.

LavaFlow belongs to the project's owner. For anyone else, every command says
"project not found", as for a project that doesn't exist, and exits with
code 4. A project access token can run the read commands for its own
project, but only the owner's own session can turn LavaFlow on or off,
change its settings and start runs: `enable`, `disable`, `settings set` and
`runs start` refuse a project access token before they ask or send anything,
with code 5. Sign in with
`volcano login` instead, and if `VOLCANO_TOKEN` holds a project access token,
unset it first.

When the owner's plan can't be read, a command says "LavaFlow couldn't read
the project owner's plan; try again later" and exits with code 5. A change
that `enable`, `disable` or `settings set` sends can also be refused for a
read-only account; the API doesn't tell that apart from a plan it couldn't
read, so the command names both, adds what Volcano said, and exits with
code 5. `runs start` does the same for a read-only or inactive account and a
plan it couldn't read (see [Start a run](#start-a-run)).

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
Ungated workflows: not allowed: LavaFlow holds a fix that a workflow could run with the repository's secrets before a person approves it
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
Merging: available for this project, but off at this autonomy, which never merges; merging also needs the base branch to require a check
Repository: acme/shop
Ungated workflows: not allowed: LavaFlow holds a fix that a workflow could run with the repository's secrets before a person approves it
Custom context: not set
System prompt: not set
Updated: 2026-10-30 12:00Z
```

Times are in UTC, marked `Z`, in every `lavaflow` command.

While LavaFlow is off, the status says what it does and how to turn it on.
When you use a project access token, which can read but not change the
settings, the status says so instead.
When it is set to open pull requests and no repository is connected, the
status says to run `volcano git connect` and then, as the owner,
`volcano cloud lavaflow enable`: LavaFlow starts no run for a repository other
than the one it was turned on for, so after you connect or change the
repository, turn it on again. When LavaFlow isn't available for
a project that has LavaFlow records, the status notes that it can't be turned
on or changed, and that the owner can still turn it off.

`Merging` says whether LavaFlow can merge its fixes for the project; `settings`
shows it too. At autonomy `merge` it reads
`available; LavaFlow holds every fix unless the base branch requires a check`:
LavaFlow merges a fix only into a base branch that requires a check, and the
CLI can't tell whether yours does. At `open_pr` or `diagnose_only`, merging is
off whatever the project allows. While it reads `not available for this project`,
`settings set --autonomy merge` is refused. When merging isn't available while
LavaFlow is on and available at autonomy `merge`, merging is paused, and it
reads:

```text
Merging: paused; waiting fixes resume when it's back, new fixes open as pull requests for review
```

A fix already waiting merges once merging is back, and a new fix opens its
pull request for you to review. The line is left out when the
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

At autonomy `merge`, LavaFlow can merge its fix pull requests. A fix built on
a commit the deploy reported, at either
[verification level](deploy.md#verification), is always held for a person.
Only a fix on a commit that a push to the repository deployed is free of that
hold. A fix it holds or stops is yours to review and merge on GitHub.

```bash
volcano cloud lavaflow settings set --autonomy merge
```

Turning on merge lets LavaFlow's fixes, including those from runs it starts
on its own, run your CI with the secrets your pull request workflows get.

`--autonomy merge` prints that and asks first. `--yes` skips the question but
still prints it, on stderr, so `--json` output stays JSON. Without a terminal
and without `--yes`, it changes nothing and exits with a non-zero code.

Only the project's owner can turn merging on, signed in with `volcano login`
while that sign-in lasts, or with a platform token that has an expiry. Under
`merge`, the same goes for `enable` and for changing the human review paths,
`--custom-context` or `--system-prompt`. The owner's GitHub account must also
be able to push to the project's repository. Volcano refuses these changes
with `person_required` for a platform token that never expires, a sign-in that
ended, an owner whose GitHub account can't push to the repository, or a
project with no repository. The command says why in Volcano's words and exits with code 5.
[Checking that the owner can push](/platform/projects/git-deploy#check-that-the-owner-can-push)
says why the owner can't. While `VOLCANO_TOKEN` is set, its token is used
instead of a `volcano login` sign-in. A project access token is refused before
anything is sent, also with code 5. Merging applies to the
repository the project is connected to when you turn it on: once the project
is connected to another, autonomy reads `open_pr` until you turn merge on
again. Merging is only for a project where
it's available, which `status` shows; elsewhere `--autonomy merge` says
"LavaFlow merging isn't available for this project yet" and exits with
code 1. When `status` shows merging isn't available while LavaFlow is, it
says so before it warns or asks. A project already at `merge` can still save
`--autonomy merge` again.

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

`show` prints `Merge:` in its `Fix:` block with where the merge is:
`waiting for CI`, `queued to merge`, `merged`, `held` or `stopped`. A merged
fix says `merged by LavaFlow`, or `merged by someone else` when a person
merged it while LavaFlow waited. While merging is paused, a
waiting fix says `paused while merging is off; it resumes once merging is back`
instead. `held` and `stopped` are final: LavaFlow doesn't touch the pull
request again, and it's yours to review and merge on GitHub. A held fix lists
each reason in Volcano's words, with the paths it names when the reason
doesn't already name them. While the pull request is open, the `Fix:` block
ends with a line saying it's yours and what to do next, and both `list` and
the `Status:` line of `show` read `Held: yours to merge` or
`Stopped: yours to merge`. Once someone merges or closes the pull request,
`show` keeps the `Merge:` line and its reasons, adds
`then someone else merged it` or `then someone closed it`, and the status
reads as the run's own.

```text
Fix:
  Pull request: #42 https://github.com/acme/shop/pull/42
  State: open
  Opened: 2026-10-30 09:03Z
  Merge: held
    - Changes db/migrations/0042_orders.sql, which always needs a person.
  The pull request is now yours to review and merge on GitHub.
```

A stopped fix says why after `stopped:`, and what to do next when that isn't
reviewing and merging the pull request:

| `merge.reason` | `Merge: stopped:` | Next step |
| --- | --- | --- |
| `review_unresolved` | LavaFlow's review didn't pass the fix | Decide whether to finish it |
| `checks_failed` | a check failed | Fix the failing check |
| `checks_timed_out` | the checks didn't finish in time | Rerun the checks |
| `blocked_by_rules` | the repository's rules block the merge | |
| `not_mergeable` | GitHub can't merge the pull request, for example because it conflicts with its base branch | Resolve the conflict |
| `branch_behind` | the base branch requires the fix to be up to date with it, and LavaFlow doesn't update fix branches | Update the branch |
| `removed_from_queue` | the fix left the merge queue, and LavaFlow can't tell why, for example because its checks failed there or someone removed it | |
| `reads_failed` | LavaFlow kept failing to read the pull request, or the project's settings or repository | Check whether it is still in the merge queue |
| `handed_off` | someone else took over the pull request, by drafting it, closing it or moving it to another branch | |
| `unreadable` | LavaFlow couldn't read the pull request's state on GitHub | |
| `permission_missing` | the Volcano GitHub App, or the project owner's GitHub account, lacks a permission the merge needs | Grant the App's requested permissions, and make sure the owner can push, so later fixes can merge |
| `repository_changed` | the project now deploys from another repository | |
| `settings_changed` | the project's settings no longer allow LavaFlow to merge | |
| `request_rejected` | GitHub refused LavaFlow's request to start the fix's checks | |
| `ci_start_commit_mismatch` | GitHub reports a different commit from the one LavaFlow made to start the fix's checks | |
| `branch_refused` | LavaFlow starts checks only on its own branch of an open pull request it opened, never the default or production branch, so it didn't start them here | |
| `head_unverified` | GitHub doesn't report the fix's head as the signed commit LavaFlow pushed, so LavaFlow didn't start its checks | |
| `checks_not_started` | LavaFlow couldn't give the fix a commit message that runs CI | |
| `queue_carries_ci_skip` | the repository's merge queue could carry an instruction to skip checks into the base branch | Follow LavaFlow's comment on the pull request to merge it without skipping checks |
| `merge_carries_ci_skip` | merging would carry an instruction to skip checks into the base branch, for example because the repository allows no squash merge | Follow LavaFlow's comment on the pull request to merge it without skipping checks |
| `paused_too_long` | merging stayed paused for more than 7 days | |
| `cancelled` | the project is being deleted, so LavaFlow stopped waiting on the merge | |

A fix that merged and waits for you to deploy it (`manual_deploy`) reads
`merged by LavaFlow; waiting for you to deploy it`. A reason your CLI doesn't
know shows its code in parentheses.

### Allow ungated workflows

By default, LavaFlow doesn't open a fix's pull request where a workflow in
the repository could run it, or put its text into a command, with the
repository's secrets before a person approves it: the fix stays on its run
instead. Only the project's owner can turn that on, signed in as a person:
with `volcano login`, or another platform token that expires. It applies to the
repository the project is connected to now, while their GitHub account can push
to it:

```bash
volcano cloud lavaflow settings set --allow-ungated-workflows true
```

Workflows in the repository may then run LavaFlow's pull requests, or put
their text into commands, with the repository's secrets before a person
reviews them. LavaFlow writes those changes from the project's error logs,
which its users can influence. Turn it on only if those workflows keep secrets
away from untrusted code; gating them on a label a person adds is safer.

Connecting the project to another repository, or disconnecting it, turns the
setting off, and connecting the first repository again doesn't turn it back
on.

`settings set` prints that warning and asks before it turns the setting on.
`--yes` skips the question but still prints the warning, on stderr, so
`--json` output stays JSON. Without a terminal and without `--yes`, it changes
nothing and exits with a non-zero code. When Volcano refuses it, for example
for a platform token that never expires, or while the owner's GitHub account
can't push to the repository, the command says why in Volcano's words and
exits with code 5. Any of the owner's credentials turns it off, with
`--allow-ungated-workflows false`, which asks nothing. While LavaFlow isn't
available for the project, Volcano turns the setting off only together with
LavaFlow, so the command turns LavaFlow off too and says so on stderr.
`status` and `settings` show it as `Ungated workflows:`, `allowed for the
connected repository` or `not allowed`, followed by what that means.

When a run's pull request opened past ungated workflows, `show` lists the
first ungated workflow job on each branch or commit LavaFlow checked: its file,
the branch or commit whose workflows run it, the job and the event that runs
it. Other jobs on the same branch or commit may run the pull request too.

```text
Ungated workflows: these jobs, and maybe others, may run this pull request with the repository's secrets before a person reviews it. LavaFlow opened it anyway, as the project's owner allows.
  - .github/workflows/preview.yml on main: job deploy-preview, runs on pull_request_target
  Gate them on a label a person adds.
```

A workflow file that couldn't be read shows as "couldn't be read".

When LavaFlow held a fix instead, because ungated workflows aren't allowed,
`show` lists every job that held it under the `Fix:` line, with the ways
forward:

```text
Fix:
  No fix pull request was opened: these workflow jobs could run the fix with the repository's secrets before a person approves it:
    - .github/workflows/preview.yml on main: job deploy-preview, runs on pull_request_target
  Gate those workflows on a label a person adds, or have the project's owner allow ungated workflows with volcano cloud lavaflow settings set --allow-ungated-workflows true; then start a new run with volcano cloud lavaflow runs start 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e. Or apply the fix yourself from the diff below.
```

When LavaFlow couldn't read every workflow, it names no job. When it read them
again and found none, they may have changed since the hold, and a new run
checks them afresh.

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
Type              Name                            Last analyzed      Issues  Fix PRs
-----------------------------------------------------------------------------------------------------
function          checkout                        2026-10-30 09:00Z       6        3  issue rate jumped
durable function  nightly-report                  never                   0        0
```

## Start a run

LavaFlow starts runs on its own only for issues that hold an exception your
code threw or logged. Start one yourself for any other
issue of the project, or for one recorded before LavaFlow was turned on. Name
the issue by its ID, such as the `Issue` that `show` prints for an earlier
run, or an ID from the [issues](/platform/guides/issues) API. The run follows
the project's settings and starts `queued`.

```bash
volcano cloud lavaflow runs start 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e
```

```text
✓ LavaFlow run started
Run: 0d6e9b1a-2f4c-4e8d-9a7b-3c5d1e2f4a6b
Status: Queued: waiting to start
Follow it with volcano cloud lavaflow show 0d6e9b1a-2f4c-4e8d-9a7b-3c5d1e2f4a6b
```

`--json` prints the run as the API returned it.

Only the project's owner can start a run, from a live sign-in, while LavaFlow
is on: a sign-in from `volcano login`, or a platform token that expires. When
the run doesn't start, the command says what to do:

| When | The command says | Exit code |
|---|---|---|
| LavaFlow is off | Turn it on with `volcano cloud lavaflow enable` | 1 |
| The project no longer links the repository LavaFlow was turned on for | Connect it again with `volcano git connect`, or turn LavaFlow on again | 1 |
| The project links another repository | Turn LavaFlow on again to fix issues in it | 1 |
| The issue has a run in progress | The run's ID and the `show` command for it, or `list` when it isn't among the newest 100 runs | 1 |
| The issue started 5 runs, or the project 200, in the last 24 hours | When to try again | 1 |
| The Volcano API serves LavaFlow but is older than starting runs | This Volcano API can't start LavaFlow runs yet | 1 |
| The project has no such issue | The project has no issue with that ID, and to use the `Issue` that `show` prints for one of its runs, or an ID from the project's issues | 4 |
| The token never expires, or its sign-in ended | Run `volcano login`, or use a platform token that expires | 5 |
| The owner's account is read-only or inactive, or their plan couldn't be read | All three, and what Volcano said | 5 |

## Follow the runs

A run works on one issue. `list` shows the newest first, with its status,
what came of it, the resource, when it was queued, and the run's title, last
and whole, so two titles that start alike still read apart. A run's title is
the title of the fix it proposed, or its diagnosis summary; until LavaFlow has
either, the title shows `-`. Narrow it with
`--status` (`queued`, `diagnosing`, `fixing`, `reviewing`, `pr_open`,
`completed`, `failed` or `cancelled`) or `--resource` with an ID from
`resources --json`. When there are more, the
list ends with the command for the next page. With `--json`, pass
`next_cursor` to `--cursor`, with the same `--limit`, `--status` and
`--resource`.

```bash
volcano cloud lavaflow list
```

```text
ID                                    Status                           Outcome                         Resource                  Queued             Title
----------------------------------------------------------------------------------------------------------------------------------------------------------------
9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c  PR open                          #42 open                        checkout                  2026-10-30 09:00Z  Load the cart before checkout reads it
0d6e9b1a-2f4c-4e8d-9a7b-3c5d1e2f4a6b  Finished                         No PR: no fix in the code       orders                    2026-10-29 14:12Z  Orders time out while the warehouse API is down

Showing 2 run(s)
```

The status says where a run is: "Queued: waiting to start" (`queued`),
"Running: finding the root cause" (`diagnosing`), "Running: writing the fix"
(`fixing`), "Running: reviewing the fix" (`reviewing`), "PR open"
(`pr_open`), "Finished" (`completed`), "Failed" (`failed`) or "Cancelled"
(`cancelled`). A held or stopped fix whose pull request is open reads
`Held: yours to merge` or `Stopped: yours to merge`. A status the CLI doesn't
know prints as the API gives it.

`Outcome` gives the run's pull request and its state, such as `#42 merged`,
or why it opened none in a few words, such as `Diagnosed only`,
`No PR: no fix in the code` or `No PR: ungated workflows`; `show` says it in
full, with what to do next. A run still at work, or one that failed or was
cancelled, shows `-`. A run whose resource name wasn't recorded shows
`(name not recorded)`.

`show` prints the commit a run read the code at, its review, a `Fix:` block,
and its diagnosis and root cause with the evidence behind them. A run that
failed or was cancelled says why on its `Fix:` line.

A run with a fix has a `Checks:` line. It says, in Volcano's words, whether
LavaFlow ran your repository's own checks, such as its tests, on the fix
before it opened the pull request or held the fix, and what they found: they
passed, they failed, LavaFlow found no checks to run, or they didn't run and
why. Your repository's CI may still run on the pull request after it opens.

When the checks ran, each command follows in the order it ran, with how it
ended (`passed`, `failed` or `timed out`) and its exit code. A command that
failed or timed out shows the end of what it printed, indented under
`Output:`:

```text
Checks: LavaFlow ran the repository's checks before opening this pull request, and they failed.
  - passed, exit code 0: npm run lint
  - failed, exit code 1: npm test
    Output:
      FAIL src/cart.test.ts
        charges nothing for an empty cart
          Expected: 0
          Received: 12.5
  - timed out: npm run e2e
    Output: (empty)
```

A command that ended without an exit code, as one that timed out may, shows
none, and `Output: (empty)` marks one that printed nothing. A status your CLI
doesn't know shows as `failed`. Volcano replaces a command or output that may
contain a credential with a sentence saying it left it out. In `show`'s text
output, a control character or a bidirectional text control in a command or
its output prints as a visible escape such as `\x1b` or `\u202e`.
`show --json` prints commands and their output as the API sent them, encoded
as JSON strings.

When the checks failed and LavaFlow then revised the fix once without running
them again, the line says so, and the commands listed are the ones that ran on
the earlier version.

When the checks didn't run, the line says why, such as
`LavaFlow didn't run the repository's tests before opening this pull request.`

When the review ends without passing the fix, a `Review stopped:` line says
why, in the words of the API's `review.stopped`:

| `review.stopped` | `Review stopped:` |
| --- | --- |
| `rounds` | The last review round still found problems. |
| `no_revision` | LavaFlow gave no revised fix. |
| `budget_exhausted` | The run used its token budget before LavaFlow could revise the fix, not the daily spend limit. |
| `context_limit` | The fix's conversation grew too long for LavaFlow before it could revise it. |
| `timed_out` | The run's time for review ran out. |
| `reviewers_failed` | No reviewer of the last round finished. |
| `too_few_reviewers` | Fewer reviewers could run than a round needs to pass the fix. |
| `unreadable` | A retried run couldn't read the review saved with its fix, so a person must review it. |

A reason your CLI doesn't know shows as `Other` with its code. When no
reviewer could run, the review reads `Review: error; no reviewer could run`,
followed by `Review stopped:` with the reason.

A review that didn't pass the fix lists, in the reviewer's words, the
findings it left open and what it leaves to you to decide before the fix
merges:

```text
Review: blocked after 3 rounds
Review stopped: The last review round still found problems.
Open findings:
  - major, src/cart.ts:42: An empty cart still reaches chargeCustomer, which reads cart.total before the new guard runs.
  - minor: LavaFlow left this explanation out because it may contain a credential
For you to decide:
  - billing: The fix skips the charge for an empty cart. Is that intended?
  - trust boundary: LavaFlow left this question out because it may contain a credential
```

Each finding gives its severity (`nit`, `minor`, `major` or `critical`) and
the file and line it points to. A finding about no file names none, and
`path not shown` marks a file whose path Volcano doesn't show. Each decision
names what it's about: `irreversible`, `trust boundary`, `breaks a contract`,
`billing`, `unexplained` or `not reviewable`. A kind your CLI doesn't know
shows as the API gives it. Volcano replaces an explanation or question that may
contain a credential with the sentence shown above. A review with nothing to
list prints neither heading.

The `Source:` line gives the commit's short SHA and where it came from:

| Shown | Meaning |
| --- | --- |
| `deployed from a push` | A push to the project's connected repository deployed it. |
| `reported by the deploy; deployed files match` | The deploy reported it, and Volcano matched the deployed files against it. |
| `reported by the deploy; found on the production branch` | The deploy reported it, and Volcano confirmed only that the production branch holds it: the [`repository` level](deploy.md#verification) function and durable function deploys get. Volcano doesn't compare the deployed files with it. A fix on such a commit opens only while the production branch still holds it. |
| `production branch; may not be deployed` | A reported problem's run read the production branch at a commit it pinned. |

A run recorded before Volcano kept the origin, or with an origin your CLI
doesn't know, shows the commit alone. A run that read no code, such as one
that diagnosed from logs alone, has no `Source:` line.

The `Fix:` block always says what happened to the run's fix:

- With a pull request: its link, its state (`open`, `draft`, `merged` or
  `closed`), when it opened, where its merge is (see
  [Merge fixes](#merge-fixes)), when it merged, and when the merged fix went
  live, with whether its issue is resolved or came back. A draft says why it's
  one, such as LavaFlow's review not passing the fix. A pull request opened
  before LavaFlow recorded the time has no open time.
- With none: why LavaFlow opened none, and what to do next, with the command
  to run where one helps, such as `settings set --autonomy open_pr` for a run
  that only diagnosed, or `list --status pr_open` when another LavaFlow pull
  request for the issue was open. A run that finished without a recorded
  reason says `No fix pull request was opened. LavaFlow didn't record why.`
- With none because LavaFlow couldn't read the code that served the error:
  the reason the API gives, the deployment that served the error when the API
  names it, and the next step, such as committing and pushing changes and then
  running `volcano cloud deploy` from a clean checkout. A reason your CLI
  doesn't know names no cause and links
  [Why LavaFlow read no code](/platform/guides/lavaflow#why-lavaflow-read-no-code).
- For a run that failed or was cancelled: why, in Volcano's words. A
  cancelled run also gives the `runs start` command to try again.
- While the run is queued or working: `No fix yet: LavaFlow is still working on this run.`

```text
Fix:
  No fix pull request was opened: the files of the deployment that served the error differ from its commit, as when the checkout had uncommitted changes.
  Deployment: 7c2e5a1f-3b8d-4e6a-9f0c-1d2e3f4a5b6c
  Commit and push those changes, then run volcano cloud deploy from the clean checkout. LavaFlow starts a new run when the error happens again after that deploy.
```

```text
Fix:
  Pull request: #42 https://github.com/acme/shop/pull/42
  State: merged
  Opened: 2026-10-30 09:03Z
  Merge: merged by LavaFlow
  Merged: 2026-10-30 09:15Z
  Deployed: 2026-10-30 09:20Z; the issue is resolved
```

```bash
volcano cloud lavaflow show 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
```

```text
Run: 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c
Status: PR open
Resource: function checkout
Title: Load the cart before checkout reads it
Issue: 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e
Source: commit 4f1c2b3 (deployed from a push)
Queued: 2026-10-30 09:00Z
Finished: 2026-10-30 09:03Z
Review: passed after 2 rounds
Checks: LavaFlow didn't run the repository's tests before opening this pull request.
Fix:
  Pull request: #42 https://github.com/acme/shop/pull/42
  State: open
  Opened: 2026-10-30 09:03Z

Diagnosis
  Checkout reads the cart before it loads.
```

`show --json` and `list --json` print the run as Volcano's API returned it,
including fields newer than your CLI.
