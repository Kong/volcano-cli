---
title: "Feedback"
description: "Report a problem with Volcano from the CLI, as a person or a coding agent, and what each report contains."
---

## What it is

`volcano feedback` sends the Volcano team a report about a problem you hit
with Volcano: the CLI, the platform, the docs, or the agent skills. The Volcano
agent skills tell coding agents to run it after they finish a task, for each
distinct Volcano problem they ran into. You can run it yourself too.

When a coding agent runs a `volcano` command that fails, feedback is on, and
you are logged in with your account, the error ends with one more line on
stderr:

```text
Coding agents: if this is a problem with Volcano itself, report it after your task with `volcano feedback`. See `volcano feedback --help`.
```

It does not appear after a usage error, such as an unknown flag or command,
after an error that already says to log in again or to upgrade the CLI, or
after a failed `volcano feedback`. Turning feedback off (below) removes it.

## Send a report

```bash
volcano feedback \
  --category docs_mismatch \
  --summary "volcano-platform skill passes --json to functions list; the CLI rejects it" \
  --details "Listing functions to check a deploy, I ran the command the skill shows." \
  --command "volcano functions list --json" \
  --expected "JSON list of functions" \
  --actual "Error: unknown flag: --json" \
  --workaround "Parsed the table output instead."
```

Sending a report requires `volcano login`. A project access token cannot send
one. See [When a report is not sent](#when-a-report-is-not-sent).

| Flag | Required | Limit | Content |
|---|---|---|---|
| `--category` | yes | | One of the categories below |
| `--summary` | yes | 120 characters | One line describing the problem |
| `--details` | yes | 4000 characters | What you were doing and what happened |
| `--command` | no | 500 characters, up to 10 | A command that shows the problem; repeat the flag for each |
| `--expected` | no | 1000 characters | What you expected |
| `--actual` | no | 4000 characters | What happened, with the exact error text |
| `--workaround` | no | 2000 characters | What you did instead |
| `--input` | no | | The report as a JSON object, a file that holds it, or `-` for stdin, instead of the flags above |
| `--dry-run` | no | | Print the report and send nothing |

## Send a report as JSON

`--input` takes one JSON object instead of the flags: the object itself, a
file that holds it, or `-` for stdin. The keys are `category`, `summary`,
`details`, `commands` (a list), `expected`, `actual`, and `workaround`, with
the same limits as the flags. Keys are case-sensitive. An unknown key, a key
written in other case such as `Details`, or a repeated key is an error, and
`--input` cannot be combined with the other report flags.

For example, `report.json` holds:

```json
{
  "category": "docs_mismatch",
  "summary": "volcano-platform skill passes --json to functions list; the CLI rejects it",
  "details": "Listing functions to check a deploy, I ran the command the skill shows.",
  "commands": ["volcano functions list --json"],
  "expected": "JSON list of functions",
  "actual": "Error: unknown flag: --json",
  "workaround": "Parsed the table output instead."
}
```

Send it in any of these forms:

```bash
volcano feedback --input '{"category": "docs_mismatch", "summary": "...", "details": "..."}'
volcano feedback --input report.json
cat report.json | volcano feedback --input -
```

An inline object is the shortest form. Escape it in two steps:

1. For JSON, write `\` as `\\`, `"` as `\"`, a line break as `\n`, and a tab
   as `\t`. Leave out terminal color codes and other control characters.
2. For the shell, replace every `'` with `'\''`, such as `can'\''t`. Inside
   single quotes a `'` ends the string, so this ends it, adds one literal `'`,
   and starts a new one.

A file or stdin needs only the JSON escapes.

## Categories

| Category | Use when |
|---|---|
| `cli_bug` | The CLI crashed, returned the wrong exit code, or printed an error that names the wrong cause |
| `docs_mismatch` | Docs, skills, or `--help` disagree with what the CLI or platform does |
| `missing_capability` | No command does what the task needed, so you edited files by hand or called the API directly |
| `platform_error` | A server error that happens again on retry |
| `confusing_ux` | You got it right only after several wrong attempts |
| `instruction_conflict` | Two Volcano instruction sources say different things |

Do not report bugs in your own application code, typos, a network error that
worked on retry, documented behavior such as `volcano login` waiting for
browser approval, or style preferences.

## What is sent

Each report carries the text you wrote and these fields:

- CLI version, operating system, and CPU architecture
- The coding agent that ran the command, whether the Volcano plugin is
  installed for it, and the plugin version
- A random report ID and the time
- Your Volcano login, which the server checks

While telemetry is on, it also carries the selected project ID and the random
machine ID that telemetry uses, and the report is linked to your account. With
telemetry off and feedback turned on with `volcano feedback enable`, the report
carries neither ID and is not linked to your account.

Before sending, the CLI replaces your home directory with `~`, strips terminal
color codes, and removes what it removes from telemetry error messages: bearer
tokens, JWTs, Volcano keys and tokens, passwords in URLs, email addresses, and
URL query strings. Other secrets, such as a `password=` value or a cloud
provider key, are not removed, and neither are names. So redact them yourself:
write `<secret>` for every password, token, or key, and placeholders such as
`<function>`, `<path>`, or `<payload>` for project, function, database, and
bucket names, file paths, and payload values. Do not put file contents,
environment variable values, application data, or customer names in a report.

## When a report is not sent

`volcano feedback` sends nothing, prints nothing, and exits 0 when:

- Feedback is off. See [Turn it off](#turn-it-off).
- You are not logged in, or are logged in only with a project access token.
- The first-run notice has not been shown yet. The first `volcano` command on a
  machine shows it, and reports are sent from the next command on. Where
  `~/.volcano/config.json` cannot be saved, such as a read-only home, the
  notice is never recorded, so no report is sent.

It exits 0 so a coding agent does not retry. `volcano feedback --dry-run` prints
the report and, when a real run would send nothing, says `This report would not
be sent.` `volcano feedback status` says which case applies.

## Turn it off

Feedback follows telemetry. While telemetry is off, with `volcano telemetry
disable` or `VOLCANO_TELEMETRY_DISABLED=1`, feedback is off too, unless you
turn it on with `volcano feedback enable`. To turn feedback off while telemetry
stays on, run:

```bash
volcano feedback disable    # saved in ~/.volcano/config.json
export VOLCANO_FEEDBACK_DISABLED=1
```

`VOLCANO_FEEDBACK_DISABLED=1` turns feedback off even after `volcano feedback
enable`, and suits a CI job or container whose home starts empty on every run.
`volcano feedback status` shows the current setting, and `volcano feedback
enable` turns it back on, also while telemetry is off.
