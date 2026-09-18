---
title: "Variables"
description: "Environment variables (including secrets) made available to your project's functions and frontends at runtime."
---

## What it is

Environment variables (including secrets) made available to your project's
functions and frontends at runtime.

## How it relates

- Belongs to a **project**.
- Consumed by **functions** and **frontends** at runtime.
- Sourced from an env file (`volcano/volcano.env` or `./volcano.env`) or the
  [declarative config](project-configuration.md). When declared, variables
  are **fully synced** on deploy: entries absent from the source are deleted.

## CLI operations

| Operation | Command |
|---|---|
| Deploy from env file | `volcano variables deploy [-f <path>]` |
| List | `volcano variables list` |
| Get | `volcano variables get <name>` |
| Delete | `volcano variables delete <name>` |

Prefix with `cloud` to force the cloud target.

## Examples

```bash
# Deploy variables from volcano/volcano.env (or a custom file)
volcano variables deploy
volcano variables deploy -f ./secrets.env

# Inspect and remove
volcano variables list
volcano variables get STRIPE_SECRET_KEY
volcano variables delete STRIPE_SECRET_KEY
```

Deploying variables triggers a rollout to the affected functions and frontends.

## Reserved names

Volcano Functions use AWS Lambda. The CLI rejects these AWS Lambda reserved
variable names before it uploads an env file or declarative config:

- `_HANDLER`
- `_X_AMZN_TRACE_ID`
- `AWS_DEFAULT_REGION`
- `AWS_REGION`
- `AWS_EXECUTION_ENV`
- `AWS_LAMBDA_FUNCTION_NAME`
- `AWS_LAMBDA_FUNCTION_MEMORY_SIZE`
- `AWS_LAMBDA_FUNCTION_VERSION`
- `AWS_LAMBDA_INITIALIZATION_TYPE`
- `AWS_LAMBDA_LOG_GROUP_NAME`
- `AWS_LAMBDA_LOG_STREAM_NAME`
- `AWS_ACCESS_KEY`
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `AWS_SESSION_TOKEN`
- `AWS_LAMBDA_RUNTIME_API`
- `LAMBDA_TASK_ROOT`
- `LAMBDA_RUNTIME_DIR`
- `AWS_LAMBDA_MAX_CONCURRENCY`
- `AWS_LAMBDA_METADATA_API`
- `AWS_LAMBDA_METADATA_TOKEN`

See the [AWS Lambda reserved environment variable list](https://docs.aws.amazon.com/lambda/latest/dg/configuration-envvars.html).
