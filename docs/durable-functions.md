---
title: "Durable functions"
description: "A durable function checkpoints its progress and resumes from the last completed step, so one execution can run for hours instead of the seconds a normal invocation allows."
---

## What it is

A durable function checkpoints its progress and resumes from the last completed
step, so one execution can run for hours instead of the seconds a normal
invocation allows. Use one for work that has to survive a restart: a multi-step
order pipeline, a long agent run, a nightly batch that calls out to a slow API.

Durable functions are cloud-only and JavaScript/TypeScript-only, and they are a
separate collection from standard functions:

- You **start** an execution instead of invoking a function. The start returns a
  handle immediately; the result arrives later.
- Each **execution** is a resource with its own id, status, and result.
- The two collections never accept each other's names or ids. A function's kind
  is fixed when it is created.

## How it relates

- Belongs to a **project**, and reads **variables**, **databases**, and
  **storage** in that project like any function.
- Its sources live in `volcano/functions/` beside standard ones. Only
  `volcano-config.yaml` says which are durable, so `volcano functions deploy
  --all` skips them and `volcano cloud durable deploy --all` picks them up.
  `-f` deploys one the manifest does not mention, and refuses a name the
  manifest declares standard — a kind cannot be changed once the function
  exists.
- Every region the project deploys to has to offer durable execution, or the
  deploy is refused up front.
- Durable execution needs the durable authoring API, so a source whose runtime
  does not offer one is refused before anything is uploaded.

```yaml
version: 1
project:
  name: my-app
functions:
  - name: hello
  - name: order-pipeline
    kind: durable
```

A durable entry takes `variable_scope` and `variables` like any other, and
`durable deploy` sends what the manifest declares. Declaring nothing leaves an
existing function's scope alone.

## CLI operations

| Operation | Command |
|---|---|
| Deploy all declared, or one | `volcano cloud durable deploy --all \| -f <name\|path> [--public \| --private]` |
| List | `volcano cloud durable list [--page 1] [--limit 100]` |
| Get | `volcano cloud durable get <name-or-id>` |
| Delete | `volcano cloud durable delete <name-or-id> [--yes]` |
| Logs | `volcano cloud durable logs <name-or-id> --type build\|runtime [--follow] [--limit 100]` |
| Start an execution | `volcano cloud durable start <function> [--input …] [--name …]` |
| List executions | `volcano cloud durable executions list <function> [--status …] [--page 1] [--limit 100]` |
| Get one execution | `volcano cloud durable executions get <function> <execution-id>` |
| Stop an execution | `volcano cloud durable executions stop <function> <execution-id> [--yes]` |
| Schedule executions | `volcano cloud durable schedulers create <function> --cron "0 * * * *" [--name …] [--input …] [--regions …]` |
| List schedulers | `volcano cloud durable schedulers list <function>` |
| Pause or resume one | `volcano cloud durable schedulers disable\|enable <function> <scheduler-id>` |
| Delete one | `volcano cloud durable schedulers delete <function> <scheduler-id> [--yes]` |

Wherever a command takes a function, it takes the name or the id. `deploy` needs
exactly one of `--all` and `-f`. `--yes` skips the confirmation prompt the three
destructive commands ask for. Schedulers are a Pro capability, capped at 5 per
project across standard and durable functions together; a create beyond that
answers `403`.

All of them run under `volcano cloud`. Local development does not run durable
executions and the local server refuses to create a durable function rather than
pretending to, so a bare `volcano durable …` answers with that refusal rather
than running anything.

## Examples

```bash
# Deploy every function volcano-config.yaml declares durable
volcano cloud durable deploy --all

# Deploy one, and let anon keys start it
volcano cloud durable deploy -f order-pipeline --public

# Start an execution and follow it
volcano cloud durable start order-pipeline --input '{"order_id":4417}'
volcano cloud durable executions get order-pipeline 66666666-6666-4666-8666-666666666666

# Only the executions still running
volcano cloud durable executions list order-pipeline --status running

# End one where it is
volcano cloud durable executions stop order-pipeline 66666666-6666-4666-8666-666666666666
```

`--status` takes the status as the API spells it: `pending`, `running`,
`succeeded`, `failed`, `timed_out` or `stopped`. The dashboard labels those for
reading, so a `pending` execution appears there as "Starting".

## Logs

`durable logs` reads the same two log streams the standard collection has, on a
durable function:

```bash
# Why the deploy ended in "failed"
volcano cloud durable logs order-pipeline --type build

# What the function logged while its executions ran
volcano cloud durable logs order-pipeline --type runtime --follow
```

`--type build` reads the deployment the function is on, which is the one that
just failed. `--type runtime` spans every execution, including every resume: the
code between context operations runs again each time, so a line logged there
appears once per resume while a line inside a completed `step` does not.

`--follow` works on both, and stops at a different point for each: following
build logs ends when the deploy does, following runtime logs runs until you
interrupt it. A function that has never deployed has no build logs, and
`--type build` says so rather than printing nothing.

`volcano cloud functions logs` does not accept a durable function's name: the
two collections never accept each other's names or ids.

## Scheduling executions

A scheduler ticks on a cron expression and starts an execution instead of
invoking the function, so every tick produces an execution you can list, follow,
and stop like any other:

```bash
# Start one execution an hour, with the same input each time
volcano cloud durable schedulers create order-pipeline --cron "0 * * * *" --input '{"scope":"hourly"}'

volcano cloud durable schedulers list order-pipeline
volcano cloud durable executions list order-pipeline
```

Each tick names its execution after the run, so a tick Volcano has to retry
resolves to the execution it already started rather than beginning a second one.
Ticks draw on the same invocation allowance and concurrency cap a manual start
does; a tick that would exceed the cap fails that run rather than queueing.

`schedulers disable` stops the ticks and leaves the scheduler in place;
executions it already started keep running. `schedulers delete` removes the
scheduler and its run history, and also leaves running executions alone — stop
those with `executions stop`.

## Starting is asynchronous

`start` returns as soon as the execution is accepted, with the execution's id
and name. Read the execution to get its status and, once it has finished, its
result:

```bash
volcano cloud durable start order-pipeline --input order.json --name order-4417
volcano cloud durable executions get order-pipeline <execution-id>
```

`--input` takes inline JSON or the path to a JSON file, and is handed to the
function verbatim. Omit it to start with no input at all, which is not the same
as starting with `{}`.

`--name` is the execution's idempotency key. Starting again under a name that
already names an execution returns the existing one instead of beginning a
second, and is not charged again — so a retried start is safe. Omit it and
Volcano generates one. A name is letters, digits, `-`, `_` and `.`, up to 255
characters; anything else is refused.

A deploy finishes asynchronously, so a start that follows one straight away can
be refused while the function is still provisioning. Wait for `get` to report
`active` and start again.

## Visibility

`--public` lets a project's anon key start executions of one function;
`--private` takes that back. A public durable function is still not invocable
over HTTP the way a public standard function is — starting an execution is the
only thing the anon key can do. Polling and stopping always need a
project-scoped credential.

Omit both flags and a redeploy keeps the visibility the function already has. A
new durable function starts private.

## Stopping and deleting

`executions stop` requests the end of one execution. Cancellation happens
behind the request, so the execution it prints back often still reads `running`;
`executions get` is how you watch it reach `stopped`. Steps already completed
are not undone. Stopping one that has already finished reports the state it is
in rather than failing.

`durable delete` tears down the function and its execution history. It does not
wait for work in flight, so stop an execution you need ended first.
