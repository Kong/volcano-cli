---
title: Sandboxes
description: Execute isolated commands and manage Sandbox sessions, files, and saved templates.
---

Run a command in a temporary Sandbox:

```bash
volcano cloud sandboxes exec --preset python3.12 -- python -c 'print(42)'
```

The command prints stdout and stderr and returns the guest process's exit code. Volcano terminates the temporary session when execution finishes. Add `--json` to receive the API result, including `exit_code` and truncation flags.

For local development, start Volcano and use the same commands without `cloud`:

```bash
volcano start
volcano sandboxes presets
volcano sandboxes exec --preset node22 -- node -e 'console.log(42)'
```

Local Sandboxes require Docker. They use the selected local project's service key; anonymous access is rejected. Cloud access requires Sandbox availability and a platform user or service key authorized for the project.

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
| `--region` | `exec`, `run` | Select a region; defaults to `aws-us-east-1` |
| `--timeout` | `exec` | Command deadline in seconds; defaults to 60 |
| `--duration` | `run` | Maximum session lifetime in seconds; defaults to 3600 |
| `--request-id` | `exec`, `run` | UUID used to retry the same request safely |
| `--json` | All Sandbox commands | Print the API response as JSON |

Use a new request ID for each intent. After a network failure, retry with the original ID and identical arguments. A canceled client does not prove the remote command stopped.
