---
title: Sandboxes
description: Create isolated environments and run code through the preview Sandbox service.
---

## Availability

`volcano cloud sandboxes` uses your existing login and selected project. The
Sandbox service must be enabled for that environment; installing these commands
does not deploy the service. Run `volcano cloud sandboxes capabilities --json`
to inspect supported features and verified data centers.

Interactive shells and the local Sandbox runtime are not available yet. Local
startup remains `volcano start`; `volcano sandboxes` currently reports this
limitation instead of silently targeting the cloud.

## Create and inspect

Choose an immutable template version and a verified DC from the service:

```sh
volcano cloud sandboxes templates list --json
volcano cloud sandboxes create --template node --template-version 1 --dc <dc> --ttl 10m --json
volcano cloud sandboxes operations get <operation-id> --json
volcano cloud sandboxes get <sandbox-id> --json
```

Create returns a durable operation, not a ready sandbox. Once the operation has
succeeded, inspect its resource ID. Keep the returned `ref.generation` and use it
for sensitive operations. A generation conflict requires inspecting the resource;
the CLI never silently upgrades your handle.

## Execute code

```sh
volcano cloud sandboxes exec <sandbox-id> --generation 1 --command 'node --version'
volcano cloud sandboxes files write <sandbox-id> main.js --generation 1 --source ./main.js
volcano cloud sandboxes exec <sandbox-id> --generation 1 --command 'node main.js' --command-timeout 2m --wait 60s
volcano cloud sandboxes run --template node --template-version 1 --dc <dc> --command 'node --version'
```

`run` is one-shot: the server owns the temporary sandbox and cleanup even if you
disconnect. A known completed command uses its actual exit status. Without
`--json`, command stdout and stderr are written to the respective output streams.
Other commands print indented JSON; `--json` selects compact JSON.

The synchronous wait defaults to 10 seconds and is limited to 60 seconds. If work
is still pending, the CLI prints the operation and exits nonzero; it does not
resubmit the command. `--async` returns immediately and exits zero for accepted
work, **not completed work**. Inspect the operation, executions and logs:

```sh
volcano cloud sandboxes executions list <sandbox-id> --json
volcano cloud sandboxes executions get <sandbox-id> <execution-id> --json
volcano cloud sandboxes executions cancel <sandbox-id> <execution-id> --generation 1
```

Platform errors and unknown/pending command outcomes exit 1. Ctrl-C or the overall
`--timeout` stops the client request, not necessarily the server command. It
defaults to 10 minutes and accepts positive durations up to 24 hours. It controls
the overall command request or log stream, including response-body reads. The
request key is printed to stderr before mutations; retain it alongside operation
IDs when investigating a disconnect. The CLI never retries writes. Do not create
a new request key and replay an uncertain command. Use an existing key only with
the exact same payload after reconciling the operation.

## Files, logs and lifecycle

```sh
volcano cloud sandboxes files list <sandbox-id> . --generation 1
volcano cloud sandboxes files read <sandbox-id> main.js --generation 1 --json
volcano cloud sandboxes files delete <sandbox-id> main.js --generation 1
volcano cloud sandboxes logs <sandbox-id> --generation 1 --follow
volcano cloud sandboxes suspend <sandbox-id> --generation 1
volcano cloud sandboxes resume <sandbox-id> --generation 1
volcano cloud sandboxes terminate <sandbox-id> --generation 1
```

File paths are workspace-relative. Uploads are limited to 8 MiB and optionally
accept `--expected-digest`. Reads return JSON with base64 `content`, `next_offset`
and `eof`; use `--offset` for subsequent reads. The CLI does not overwrite a local
file or silently drop pagination metadata.

Logs are NDJSON with sequence, cursor and truncation metadata. A followed stream
ending is reported as a disconnect. Reconnect explicitly with `--cursor`; each
connection is freshly authorized, and no command is replayed.

## Usage and custom templates

```sh
volcano cloud sandboxes usage --from 2026-09-01T00:00:00Z --to 2026-09-02T00:00:00Z --json
volcano cloud sandboxes templates create --data '{"name":"example","baseline":"1gib","language":"node"}'
volcano cloud sandboxes templates get <template-id> <version>
volcano cloud sandboxes templates deploy <template-id> --data @deployment.json
volcano cloud sandboxes deployments list
```

Lists and usage support `--limit` and `--cursor`. Usage quantities stay decimal
strings or `null`; raw correction history is not a billing total.
The RFC3339 `--from` and `--to` bounds are normalized to UTC while preserving
fractional seconds up to nanosecond precision. Reuse the same bounds with each
pagination cursor.

Custom templates require the service's `custom_templates` capability. Deployment
JSON includes `template_version`, `dc`, `source_artifact_id` and `source_digest`
from an uploaded, verified source. `templates prepare-source` accepts `size_bytes`
(decimal string) and `sha256`, and returns a sensitive upload URL and required
headers; keep this output out of logs. Source packaging/upload integration is not
yet provided by the CLI. Disabled server features fail explicitly.

Use `--help` on any command for its flags. Template mutation bodies can be inline
JSON or `@file` and follow the versioned Sandbox service contract.
