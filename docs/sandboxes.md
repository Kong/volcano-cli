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

Local Sandboxes require Docker. `volcano start` starts the core services before attempting the optional Sandbox broker. A broker startup failure prints a warning and leaves the core services available; Sandbox requests remain unavailable until the broker is reachable. `volcano stop` also stops the broker.

`volcano stop --clean` removes Sandbox containers, their private networks, and custom images before deleting local data volumes, including resources left after a broker failure. Cleanup is limited to this local environment; other Docker projects and images are preserved. Networks still used by other containers are not disconnected. If Sandbox cleanup fails, data volumes are kept so you can retry `volcano stop --clean`.

Local Sandboxes use the selected local project's service key; anonymous access is rejected. Cloud access requires Sandbox availability and a platform user or service key authorized for the project.

## Keep a session

```bash
volcano cloud sandboxes run --preset python3.12 --duration 300
volcano cloud sandboxes sessions
volcano cloud sandboxes get SESSION_ID
volcano cloud sandboxes exec SESSION_ID -- sh -c 'echo hello > /workspace/message'
volcano cloud sandboxes files read SESSION_ID /workspace/message
volcano cloud sandboxes files list SESSION_ID
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

Without `--duration`, `run` inherits the template's `ttl_seconds`. The default
is 3600 seconds in cloud mode and unlimited in local mode. To override a finite
template with an unlimited local session, use
`volcano sandboxes run --template TEMPLATE_ID --duration 0`.

`exec` requires `--` before the command. Each argument is shell-quoted. For shell syntax such as pipes or background processes, explicitly invoke `sh -c`:

```bash
volcano cloud sandboxes exec SESSION_ID -- sh -c 'python -m http.server 8080 >/workspace/http.log 2>&1 &'
```

Files are binary-safe and limited to 8 MiB. Write from stdin:

```bash
volcano cloud sandboxes files write SESSION_ID /workspace/input.bin < input.bin
volcano cloud sandboxes files read SESSION_ID /workspace/output.bin > output.bin
```

## List files

List a directory in the session's workspace. Name it as `src` or
`/workspace/src`, or omit it to list the whole workspace:

```bash
volcano cloud sandboxes files list SESSION_ID src
```

```text
Type       Mode          Size  Path
-----------------------------------
file       0644            31  src/main.py
directory  0755             -  src/pkg
file       0600             5  src/pkg/data.txt
symlink    0777             -  src/pkg-link
file       0755            20  src/run.sh

Total: 5 entries
```

Paths are relative to the workspace root and work with `files read` and
`files write`. Entries are sorted by path, so a directory comes right before its
contents. The listing includes every entry below the directory; pass
`--recursive=false` for its direct entries only. A symlink is listed as
`symlink` and never followed. A path that is missing, is a file, or passes
through a symlink is refused as not a directory. A path the session user can't
list, or can't reach because a directory above it lacks execute permission, is
refused as unreadable. A credential for the session's project that lacks the
`sandboxes.files.read` permission is refused with 403; a project the credential
can't reach answers 404, like one that doesn't exist.

Add `--sha256` for each regular file's SHA-256 digest. A file without a digest
says why in its place, and is unverified:

| Reason | File |
|---|---|
| `skipped: budget` | Larger than 256 MiB |
| `skipped: changing` | Changed while it was hashed; list it again later |
| `skipped: unreadable` | Not owned by the session user, has its owner-read bit (`0400`) clear, or has more than one hard link |

The command reads every page before it prints. `--limit` sets the most entries
per request, from 1 to 10000. With `--json` it prints one JSON object with the
API's fields: `project_id`, `session_id`, every page's entries in `entries`,
and `unsupported_names` when the listed directory holds names the listing
skipped. In an entry, `mode` is the permission bits as a number (420 is
`0644`); `sha256` or `sha256_skipped` (`budget`, `changing`, or `unreadable`)
appears only with `--sha256`; `unreadable: true` marks a directory the listing
didn't enter; and `unsupported_names` counts the names skipped in a directory:

```bash
volcano cloud sandboxes files list SESSION_ID src --sha256 --json
```

A session started before file listing was available can't list its files.
Start a new session; for a custom template, redeploy the template first.

### Incomplete listings

A listing leaves out a directory's contents when the session user can't list
it, and names that are not UTF-8 or whose paths would pass 4096 bytes, with
everything below them. The table marks each such directory in the Size column:

| Marker | Directory |
|---|---|
| `unreadable` | The session user lacks read or execute permission on it; its contents aren't listed |
| `incomplete` | Holds names the listing skipped |

```text
Type       Mode          Size  Path
-----------------------------------
directory  0000    unreadable  src/locked
directory  0755    incomplete  src/pkg
file       0600             5  src/pkg/data.txt

Total: 3 entries

Warning: The listing is incomplete.
  1 directory marked unreadable was not entered: the session user lacks read or execute permission on it.
  2 names were skipped, with everything below them, because they are not UTF-8 or their paths would pass 4096 bytes (1 in the listed directory, 1 in 1 directory marked incomplete).
```

The warning goes to stderr after the table. It also counts names skipped in the
listed directory itself, which has no row. The command still succeeds. With
`--json`, the same facts are in `unreadable` and `unsupported_names`, and no
warning is printed.

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
| `--duration` | `run` | Override session lifetime in seconds: cloud 30–28800; local 0 (unlimited) or positive. Omitted inherits the template |
| `--request-id` | `exec`, `run` | UUID used to retry the same request safely |
| `--sha256` | `files list` | Add each regular file's SHA-256 digest |
| `--recursive` | `files list` | `--recursive=false` lists only the directory's direct entries |
| `--json` | All except `deployments source` | Print compact JSON; `exec`, `shell`, `files read`, and `files list` return structured results |

Use a new request ID for each intent. After a network failure, retry with the original ID and identical arguments. A canceled client does not prove the remote command stopped.

## Output format

Session, preset, template, and file-write commands always return JSON, indented by default and compact with `--json`. This JSON format is supported for scripts. `exec` and `shell` normally write guest stdout/stderr, `files read` writes raw bytes, and `files list` prints a table; use `--json` for their structured results instead. JSON execution results retain timeout and truncation flags without adding warnings to the JSON stream. `deployments source` writes the original tar.gz bytes to stdout and rejects `--json`; redirect its output to a file.

Template creation requires an explicit `--preset` from `sandboxes presets`. Shell command lines may be up to 64 KiB. For commands longer than the 60-second one-shot limit, start a session and use `exec SESSION_ID --timeout SECONDS`.

## Read Sandbox usage

```bash
volcano sandboxes usage --json
```

The response contains project totals and hourly/daily series for configured
memory times observed running duration (MiB-seconds), suspended duration
(seconds), and uncertain duration weighted by configured memory (MiB-seconds).
Confirmed cloud running usage consumes the account plan allowance and paid
overage credits. Suspended and uncertain usage are uncharged; snapshot costs
are included in runtime pricing. These project metrics are not an invoice:
allowance and purchased capacity are shared across the account.
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

`--ports` accepts at most 16 unique ports from 1 through 65532.

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
