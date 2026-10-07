---
title: Sandboxes
description: Execute isolated commands and manage Sandbox sessions, files, and saved templates.
---

## Run a command

Run a command in a temporary Sandbox:

```bash
volcano cloud sandboxes exec --preset python3.12 -- python -c 'print(42)'
```

The command prints stdout and stderr and returns the guest process's exit code (124 on timeout). Timeout and truncated stdout/stderr produce warnings on stderr in text mode. Volcano terminates the temporary session when execution finishes. Add `--json` to receive the API result, including `exit_code` and truncation flags.

For local development, start Volcano and use the same commands without `cloud`:

```bash
volcano start
volcano sandboxes presets
volcano sandboxes exec --preset node22 -- node -e 'console.log(42)'
```

Local Sandboxes require Docker. `volcano start` starts the core services before attempting the optional Sandbox broker. A broker startup failure prints a warning and leaves the core services available; Sandbox requests remain unavailable until the broker is reachable. `volcano stop` also stops the broker, and `volcano stop --clean` removes its Compose volumes. They use the selected local project's service key; anonymous access is rejected. Cloud access requires Sandbox availability and a platform user or service key authorized for the project.

## Keep a session

```bash
volcano cloud sandboxes run --preset python3.12 --duration 300
volcano cloud sandboxes sessions
volcano cloud sandboxes get SESSION_ID
volcano cloud sandboxes exec SESSION_ID -- sh -c 'echo hello > /workspace/message'
volcano cloud sandboxes files read SESSION_ID /workspace/message
volcano cloud sandboxes shell SESSION_ID
volcano cloud sandboxes suspend SESSION_ID
volcano cloud sandboxes resume SESSION_ID
volcano cloud sandboxes terminate SESSION_ID
```

Replace `SESSION_ID` with the ID returned by `run`. Creation and lifecycle changes are asynchronous: check `get` until the session is `running`, `suspended`, or `terminated` as appropriate. `run` starts a persistent session; use `shell SESSION_ID` for a command prompt. The prompt runs one command per line
and supports pipes through the remote shell. Files and background processes persist;
shell variables and `cd` changes do not. `exit` or EOF detaches without terminating
the session. Full-screen programs such as `vim` require a PTY, which this prompt
does not provide. Always terminate sessions you no longer need.

`exec` requires `--` before the command. Each argument is shell-quoted. For shell syntax such as pipes or background processes, explicitly invoke `sh -c`:

```bash
volcano cloud sandboxes exec SESSION_ID -- sh -c 'python -m http.server 8080 >/workspace/http.log 2>&1 &'
```

Files are binary-safe and limited to 8 MiB. Write from stdin:

```bash
volcano cloud sandboxes files write SESSION_ID /workspace/input.bin < input.bin
volcano cloud sandboxes files read SESSION_ID /workspace/output.bin > output.bin
```

## Save a template

```bash
volcano cloud sandboxes templates create analysis --preset python3.12 --memory 2048
volcano cloud sandboxes templates list
volcano cloud sandboxes templates get TEMPLATE_ID
volcano cloud sandboxes run --template TEMPLATE_ID
volcano cloud sandboxes templates delete TEMPLATE_ID
```

Deleting a template terminates its sessions and permanently removes their files.
The CLI asks for confirmation. In scripts, pass `--yes` to confirm that deletion.

A template saves a preset and memory size. Custom image builds are not supported by these commands. Lists return pagination metadata; pass `--cursor` to continue where supported.

## Select limits and retry safely

| Flag | Commands | Purpose |
|---|---|---|
| `--preset` | `exec`, `run`, template creation | Choose an available preset |
| `--template` | `exec`, `run` | Use a saved template ID instead of a preset |
| `--memory` | `exec`, `run` | Override memory with 1024 or 2048 MB; otherwise inherit the preset or template |
| `--region` | `exec`, `run` | Select a region; defaults to `us-east-1` |
| `--timeout` | `exec` | Command deadline: 1–60 seconds for one-shot execution, 1–3600 for session execution; defaults to 60 |
| `--duration` | `run` | Maximum session lifetime: 30–28800 seconds; defaults to 3600 |
| `--request-id` | `exec`, `run` | UUID used to retry the same request safely |
| `--json` | All except `deployments source` | Print compact JSON; `exec`, `shell`, and `files read` return structured results |

Use a new request ID for each intent. After a network failure, retry with the original ID and identical arguments. A canceled client does not prove the remote command stopped.

## Output format

Session, preset, template, and file-write commands always return JSON, indented by default and compact with `--json`. This JSON format is supported for scripts. `exec` and `shell` normally write guest stdout/stderr, and `files read` writes raw bytes; use `--json` for their API response instead. JSON execution results retain timeout and truncation flags without adding warnings to the JSON stream. `deployments source` writes the original tar.gz bytes to stdout and rejects `--json`; redirect its output to a file.

Template creation requires an explicit `--preset` from `sandboxes presets`. Shell command lines may be up to 64 KiB. For commands longer than the 60-second one-shot limit, start a session and use `exec SESSION_ID --timeout SECONDS`.

## Read preview usage

```bash
volcano sandboxes usage --json
```

The response contains project totals and hourly/daily series for configured
memory times observed running duration (MiB-seconds), suspended duration
(seconds), and uncertain duration weighted by configured memory (MiB-seconds).
These preview metrics do not debit credits and are not a price estimate.
Observation gaps and unconfirmed transitions remain uncertain. Local sessions
contribute zero cloud usage. An older server without these metrics returns a
clear error instead of showing zero.

For cloud usage, run `volcano cloud sandboxes usage --json` after signing in
with `volcano login`. Project service keys cannot read project usage.

## Deploy a custom image

Put a `Dockerfile` fragment at the root of your build context. Volcano supplies
the base image and managed entrypoint. Use `RUN`, `COPY`, and `CMD`; do not add
`FROM`, `USER`, or `ENTRYPOINT`. For a Python HTTP server:

```dockerfile
RUN dnf install -y python3.12 && dnf clean all
CMD ["python3.12", "-m", "http.server", "8080", "--bind", "0.0.0.0"]
```

Upload the build context:

```sh
volcano cloud sandboxes templates deploy my-python --path ./sandbox --memory 1024 --ports 8080
```

The response contains `template_id` and `deployment.id`. A deployment builds and
validates the image before making it active. Check its status, then create a
session from the template:

```sh
volcano cloud sandboxes deployments get <template-id> <deployment-id>
volcano cloud sandboxes run --template <template-id> --region aws-us-east-1
```

Use `--template <template-id>` on subsequent deployments to update the same
template. Existing sessions keep their original image. To retry after an uncertain
network result, preserve both the printed template ID and request ID:

```sh
volcano cloud sandboxes templates deploy my-python --path ./sandbox --template <template-id> --request-id <request-id>
```

Keep the same source, memory, and ports when reusing a request ID. A changed build
needs a new request ID. The CLI prints these IDs before uploading so they remain
available if the connection fails.

Build contexts are limited to 32 MiB compressed and expanded, including archive
headers, and 10,000 files. The CLI applies `.gitignore`, excludes `.git`, `.env`,
`.env.*`, and `node_modules`, and refuses symbolic links. The uploaded
`.dockerignore` also controls the image build. Files must be regular files within
the chosen directory.

Inspect history and download the original source without extracting it:

```sh
volcano cloud sandboxes deployments logs <template-id> <deployment-id> --region aws-us-east-1
volcano cloud sandboxes deployments list <template-id> --limit 25
volcano cloud sandboxes deployments list <template-id> --cursor <next-cursor>
volcano cloud sandboxes deployments source <template-id> <deployment-id> > source.tar.gz
```

For local development, run `volcano start` and omit `cloud` from these commands
(for example, `volcano sandboxes templates deploy my-python --path ./sandbox`).
The local server must support custom image deployments, and Docker must be running.

Deployment history accepts `--limit` (1–100, default 10) and `--cursor` to control pagination.
