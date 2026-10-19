---
title: "Notifications"
description: "Get alerted by email or a signed webhook when a function scheduler keeps failing, and configure it from the CLI."
---

## What it is

Notifications tell you when a scheduled job stops working. When a function
scheduler's runs keep failing, Volcano alerts the project by email, by a signed
webhook, or both, and again when the scheduler recovers.

A new project emails its owner after 3 failed scheduled runs in a row, with no
further setup.

## How it relates

- Belongs to a **project**. The settings cover every scheduler in it, for
  standard and durable functions.
- Driven by **schedulers**. For a durable function, a run fails when the
  execution it started fails, times out, or is stopped. For a standard
  function, a run fails when Volcano cannot invoke it; an error the function
  itself returns does not count. `schedulers list` shows each scheduler's
  health and how many runs in a row have failed.
- Declared in the **project configuration** under `notifications:`, except the
  webhook signing secret, which is never written to the manifest. See
  [project-configuration.md](project-configuration.md).

## CLI operations

| Operation | Command |
|---|---|
| Show settings | `volcano cloud notifications get [--json]` |
| Change settings | `volcano cloud notifications set [flags] [--json]` |
| Replace the webhook secret | `volcano cloud notifications rotate-webhook-secret [--yes] [--json]` |
| Send a test | `volcano cloud notifications test [--json]` |

Drop `cloud` to run the same commands against local development. Locally, a
webhook may point at `localhost` or another private address.

## Show the settings

```bash
volcano cloud notifications get
```

```text
Email: on, to the project owner
Webhook: not configured
Alert on: 3 failed scheduled runs in a row
Recovery: notify when a failing scheduler succeeds again
```

## Add a webhook

```bash
volcano cloud notifications set --webhook-url https://example.com/hooks/volcano
```

```text
✓ Notification settings saved
Email: on, to the project owner
Webhook: https://example.com/hooks/volcano
Signing secret: configured
Alert on: 3 failed scheduled runs in a row
Recovery: notify when a failing scheduler succeeds again

Signing secret: whsec_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
Warning: Copy this secret now. It is shown once and cannot be retrieved again.
```

The first webhook URL generates a signing secret, printed only by this command.
Changing the URL later keeps the secret. Use `--json` to capture it in a script.
See [Verify a webhook](/platform/projects/notifications) for the payload and
how to check its signature.

Outside local development, the URL must reach the public internet. A URL naming
a private or loopback address is refused.

## Change when alerts are sent

```bash
volcano cloud notifications set --notify-on first_failure
volcano cloud notifications set --notify-on consecutive_failures --threshold 5
volcano cloud notifications set --notify-on-recovery=false
```

| Flag | Effect |
|---|---|
| `--email` | Email alerts. `--email=false` turns email off. |
| `--recipients <a>,<b>` | Email up to 10 addresses instead of the project owner. `--recipients=` emails the owner again. |
| `--webhook-url <url>` | Post alerts to this `http` or `https` URL. |
| `--remove-webhook` | Stop posting to the webhook and delete its signing secret. |
| `--notify-on <when>` | `first_failure` alerts on a scheduler's first failed run; `consecutive_failures` waits for `--threshold` failed runs in a row. |
| `--threshold <n>` | Failed runs in a row before a `consecutive_failures` alert, 1 to 100. |
| `--notify-on-recovery` | Alert when an announced failing scheduler succeeds again. |

Flags you leave out keep their current values. A failing streak is announced
once, however long it lasts.

## Rotate the signing secret

```bash
volcano cloud notifications rotate-webhook-secret --yes
```

Every delivery from then on, including retries of earlier alerts, is signed
with the new secret, so update your receiver right away. Without `--yes` the
command asks first.

## Send a test

```bash
volcano cloud notifications test
```

```text
✓ Test email delivered
✓ Test webhook delivered (HTTP 200)
```

The test goes to every configured channel and reports whether each one
arrived. The command fails when one did not:

```text
✓ Test email delivered
Error: test webhook not delivered (HTTP 500): webhook responded 500
```

One test per project per minute.

## See which schedulers are failing

```bash
volcano cloud functions schedulers list nightly-export
volcano cloud durable schedulers list order-pipeline
```

```text
ID                                    Name                      State      Health    Fails  Cron             Next Run              Last Run
---------------------------------------------------------------------------------------------------------------------------------------------------
4f6c1d2e-8a3b-4c5d-9e7f-0a1b2c3d4e5f  nightly-export            enabled    failing   49     0 * * * *        2026-10-19T15:00:00Z  12m ago
  last error: Runtime.ImportModuleError: Cannot find module 'pg'
```

`Fails` counts the failed runs in a row, and resets when a run succeeds.
Pausing a scheduler keeps its count.
