---
title: "Feedback"
description: "Report a problem with Volcano from the CLI, as a person or a coding agent, and what each report contains."
---

## What it is

`volcano feedback` sends the Volcano team a report about a problem you hit
with Volcano: the CLI, the platform, the docs, or the agent skills. The Volcano
agent skills tell coding agents to run it after they finish a task, for each
distinct Volcano problem they ran into. You can run it yourself too.

Feedback is off whenever telemetry is off. See [telemetry.md](telemetry.md).

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

On success the CLI prints the report ID. When telemetry is off it prints
`Feedback not sent`, sends nothing, and exits 0. So does the first command after install, which shows the telemetry notice.

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
the same limits as the flags. An unknown key is an error, and `--input` cannot
be combined with the other report flags.

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

An inline object is the shortest form, but it breaks when a value contains a
single quote, such as `can't`, because the quote ends the shell string. A file
needs no shell quoting, so any text works. Use `\n` for a line break inside a JSON value.

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
- The selected project ID
- The random machine ID that telemetry uses, so the Volcano team can match the
  report to the usage events of the commands that failed
- A random report ID and the time

If you are logged in, the report is linked to your Volcano account.

Before sending, the CLI replaces your home directory with `~`, strips terminal
color codes, and removes the same secret shapes it removes from telemetry error
messages: bearer tokens, JWTs, Volcano keys, AWS access key IDs, passwords in
URLs, URL query strings, values of `password=`, `token=`, and similar pairs,
and other long opaque strings. Redaction cannot recognize every secret, so do
not put file contents, environment variable values, application data, or
customer names in a report.

Use `--dry-run` to see the exact report before sending it.

## Turn it off

Feedback follows the telemetry setting:

```bash
volcano telemetry disable
export VOLCANO_TELEMETRY_DISABLED=1
```
