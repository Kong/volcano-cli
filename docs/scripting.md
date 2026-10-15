---
title: "Scripts and agents"
description: "Exit codes and JSON output for running the Volcano CLI from scripts, CI, and coding agents."
---

## Exit codes

Every command exits with one of these codes, so a script or agent can tell
failures apart without parsing the error text.

| Code | Meaning | What to do |
|---|---|---|
| `0` | Success | |
| `1` | Any other failure | Read the error on stderr |
| `2` | Not logged in, or the API rejected the credential | Run `volcano login` |
| `3` | No project selected | Run `volcano connect`, or `volcano use <project>` |
| `4` | The project or resource was not found | Check the name or ID |
| `5` | The API refused the request, or the command needs an account token | Check the credential's access and the plan's limits |

`volcano sandboxes exec` and `volcano sandboxes shell` return the guest
process's exit code instead, and `124` on timeout. See
[sandboxes.md](sandboxes.md).

## JSON output

Commands that support `--json` print machine-readable JSON on stdout. These
include:

- `volcano projects list` and `volcano projects get`
- `volcano connect`
- `volcano cloud access-tokens …`
- `volcano cloud domains list` and `volcano cloud domains verify`
- `volcano durable approvals …`
- `volcano functions invoke`
- `volcano projects keys service …`
- `volcano sandboxes …`

Run `volcano <command> --help` to check whether a command has `--json`.

```bash
volcano projects list --json | jq -r '.data[].name'
```
