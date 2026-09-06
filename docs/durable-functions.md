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
- Every region the project deploys to has to offer durable execution, or the
  deploy is refused up front.

```yaml
version: 1
project:
  name: my-app
functions:
  - name: hello
  - name: order-pipeline
    kind: durable
```

## CLI operations

| Operation | Command |
|---|---|
| Deploy all declared, or one | `volcano cloud durable deploy [--all \| -f <name\|path>]` |
| List | `volcano cloud durable list` |
| Get | `volcano cloud durable get <name>` |
| Delete | `volcano cloud durable delete <name>` |
| Start an execution | `volcano cloud durable start <name> [--input …] [--name …]` |
| List executions | `volcano cloud durable executions list <name> [--status …]` |
| Get one execution | `volcano cloud durable executions get <name> <execution-id>` |
| Stop an execution | `volcano cloud durable executions stop <name> <execution-id>` |

There is no top-level `volcano durable …`: the local development environment
does not run durable executions, and the local server refuses to create a
durable function rather than pretending to.

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
Volcano generates one.

## Visibility

`--public` lets a project's anon key start executions of one function;
`--private` takes that back. A public durable function is still not invocable
over HTTP the way a public standard function is — starting an execution is the
only thing the anon key can do. Polling and stopping always need a
project-scoped credential.

Omit both flags and a redeploy keeps the visibility the function already has. A
new durable function starts private.

## Stopping and deleting

`executions stop` ends one execution at its next checkpoint. Steps already
completed are not undone, and work already in flight is not interrupted
mid-attempt. Stopping one that has already finished reports the state it is in
rather than failing.

`durable delete` tears down the function and its execution history. It does not
wait for work in flight, so stop an execution you need ended first.
