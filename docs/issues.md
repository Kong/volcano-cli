---
title: "Issues"
description: "List the runtime errors Volcano groups into issues, read one, and leave notes on an issue that LavaFlow's next run reads."
---

## What it is

Volcano groups the runtime errors your functions and frontends log into
issues. Each issue is one problem: every error of the same type thrown from
the same place in your code, or with the same message when there is no stack.
An issue shows how often the error happened, when it started, and whether it
came back after it was resolved.

A note on an issue tells LavaFlow what the logs don't show, such as how the
code should behave or what you already tried. LavaFlow's next run on the
issue reads the issue's notes.

```bash
volcano cloud issues list --status open
volcano cloud issues notes add 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e \
  --text "Coupons without an expiry date never expire."
```

## How it relates

- Belongs to a **project**. A **function** or **frontend** raises it. See
  [functions.md](functions.md) and [frontends.md](frontends.md).
- **LavaFlow** works on issues: each run works on one, and `volcano cloud
  lavaflow show` names it on its `Issue:` line. See [lavaflow.md](lavaflow.md).
- Local development records issues as the cloud does, so `volcano issues list`
  and `volcano issues get` read the local project's. Notes are cloud-only:
  they are input to LavaFlow, which local development doesn't run, so
  `volcano issues notes` refuses and points to `volcano cloud issues notes`.

## CLI operations

| Operation | Command |
|---|---|
| List issues, most recently seen first | `volcano cloud issues list [--status s] [--limit n] [--cursor c] [--json]` |
| Show one issue | `volcano cloud issues get <issue-id> [--json]` |
| List an issue's notes, newest first | `volcano cloud issues notes <issue-id> [--kind issue\|run] [--limit n] [--cursor c] [--json]` |
| Add a note | `volcano cloud issues notes add <issue-id> [--text t] [--run <run-id>] [--json]` |
| Replace a note's text | `volcano cloud issues notes edit <note-id> [--text t] [--json]` |
| Delete a note | `volcano cloud issues notes delete <note-id> [--yes]` |

Drop `cloud` to read the local project's issues: `volcano issues list` and
`volcano issues get <issue-id>`.

## List issues

`list` shows each issue's status, the resource that raised it, its title and
how many errors it counted, most recently seen first. Narrow it with
`--status` (`open`, `resolved` or `ignored`). When there are more, the list
ends with the command for the next page. With `--json`, pass `next_cursor` to
`--cursor`, with the same `--limit` and `--status`.

```bash
volcano cloud issues list --status open
```

```text
ID                                    Status    Resource                  Title                                      Events  Last seen
--------------------------------------------------------------------------------------------------------------------------------------------
5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e  open      checkout                  TypeError: Cannot read properties of ...       42  2026-10-30 08:59 UTC

Showing 1 issue
```

A title replaces values that change on every error, such as IDs, numbers and
quoted strings, with placeholders like `<n>` and `<str>`, so the same error
with different values is one issue. Times are in UTC.

## Show one issue

`get` prints the issue's title, status and resource, the error type and the
place in your code it was thrown from, how many errors it counted, and when it
was first and last seen. A resolved issue says when it was resolved, and one
that came back says when it reopened.

```bash
volcano cloud issues get 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e
```

```text
Issue: 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e
Title: TypeError: Cannot read properties of undefined (reading <str>)
Status: open
Resource: function checkout
Error type: TypeError
Culprit: applyCoupon (functions/checkout.js)
Exception: yes
Events: 42
First seen: 2026-10-30 08:41 UTC
Last seen: 2026-10-30 08:59 UTC
Last region: us-east-1
Note: Read its notes with volcano cloud issues notes 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e.
```

## Leave notes for LavaFlow

A note is plain text, up to 2,048 characters. LavaFlow's next run on the issue
reads the notes on the issue, newest first, as context it treats as data, not
instructions: nothing in a note changes a setting or what LavaFlow may do, and
LavaFlow never copies a note into a pull request. A note on a run explains
that run, such as why you closed its pull request. Later runs don't read notes
on a run.

### Add a note

Pass the text with `--text`, or pipe it on stdin. `add` prints one line with
the note's ID.

```bash
volcano cloud issues notes add 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e \
  --text "Coupons without an expiry date never expire. Only FALL25 has one."
```

```text
✓ Note 0e7d4c2b-8a1f-4b3e-9c5d-6f7a8b9c0d1e added. LavaFlow reads this note on the issue's next run.
```

```bash
volcano cloud issues notes add 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e < note.txt
```

`--run <run-id>` puts the note on one LavaFlow run of the issue instead. With
`--json`, `add` prints the note on stdout and the sentence about LavaFlow on
stderr. Sending the same text on the same issue or run again within 5 minutes
returns the note already written, so a retry is safe.

### Read the notes

`notes` lists the notes on an issue and on its runs, newest first. Each shows
what it is on, who wrote it, when, and its version once it was edited. Notes
read with a project access token name their author by user ID instead of
email. `--kind issue` lists only the notes LavaFlow's runs read; `--kind run`
lists only notes on runs.

```bash
volcano cloud issues notes 5b2d8e1c-3f4a-4c6b-9d7e-8a1f2b3c4d5e
```

```text
0e7d4c2b-8a1f-4b3e-9c5d-6f7a8b9c0d1e  on the issue  by dev@example.com  2026-10-30 09:10 UTC  edited 2026-10-30 09:20 UTC, version 2
  Coupons without an expiry date never expire.
  Only FALL25 has one.

Showing 1 note
```

### Edit or delete a note

`edit` replaces a note's text, which only its author can do. The old text is
gone for good, and the version goes up by one. `delete` removes a note for
good; its author or the project's owner can. It asks first, and `--yes` skips
the question. LavaFlow's runs don't read a deleted note.

```bash
volcano cloud issues notes edit 0e7d4c2b-8a1f-4b3e-9c5d-6f7a8b9c0d1e --text "Only FALL25 has an expiry date."
volcano cloud issues notes delete 0e7d4c2b-8a1f-4b3e-9c5d-6f7a8b9c0d1e --yes
```

### Who can write notes

Only the project's owner writes notes, as a person: signed in with `volcano
login`, or with a platform token that expires. A project access token reads
notes but can't write, edit or delete them: the CLI refuses it before it sends
anything and exits with code 5. A platform token that never expires is
refused too, in Volcano's words, with code 5. When `VOLCANO_TOKEN` is set, its
token is used instead of a `volcano login` sign-in, and the refusal says so.

### When Volcano refuses a note

Volcano checks a note before it stores it. A refused note isn't stored, and
the error names the check, never the text. Rephrase the note and send it
again. Never paste a secret into a note.

| Check | Refuses a note that holds |
|---|---|
| `characters` | a control, format, private-use or other invisible character |
| `size` | over 2,048 bytes once normalized to Unicode NFKC |
| `credential` | what looks like a credential, such as an API key or a token |
| `secret` | one of the project's secret values |
| `sanitizer` | text Volcano masks in logs |
| `link` | a URL, or a host name with `//`, user info, a port, a path or a query |

```text
Error: the note wasn't saved (link): it holds a link: a URL, or a host name with //, user info, a port, a path or a query. Rephrase it and send it again
```

Other refusals:

- Editing someone else's note, or deleting a note you didn't write on a
  project you don't own, says only the author can edit it, and only its author
  or the project's owner can delete it. Exit code 5.
- An issue holds up to 200 notes, run notes included, and a project up to
  20,000. At the limit, delete notes to write more.
- A project takes up to 30 note writes and edits a minute. Past that, the
  error says how long to wait.
- An issue, run or note the project doesn't have is not found. Exit code 4.

## Availability

Issues are in preview. Where they aren't available, every `volcano cloud
issues` command says "issues aren't available in this Volcano environment
yet" and exits with code 4. On a Volcano API that has no issues, it says
"issues aren't available on this Volcano API", or for notes "issue notes
aren't available on this Volcano API", and exits with code 4. Local
development always has issues.
