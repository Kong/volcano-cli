---
title: "Account sessions"
description: "List the browser and CLI sessions signed in to your Volcano account, and sign them out from the CLI."
---

Every place you're signed in to Volcano is a session: the dashboard in each
browser, and each `volcano login`. The `volcano account sessions` commands list
them and sign them out.

They need an account login. A project access token can't reach them, and the
commands say so before sending anything. They work against the cloud API:
local development keeps no sign-in sessions, so there they report that account
sessions are cloud-only.

To end the login this CLI uses, run `volcano logout`. It revokes that session
on the server and removes the login from this machine. See
[authentication.md](authentication.md).

## Listing sessions

```bash
volcano account sessions list
volcano account sessions list --json
```

Each session shows its ID, whether it is a `CLI` login or a `Browser`
session, the device, its address, and when it was last active. The session
this CLI is logged in with is marked `this CLI`. `--page` and `--limit` page
through a long list, and `--json` prints the sessions as the API returns them,
with `is_current` marking this CLI's own.

## Signing out one session

```bash
volcano account sessions revoke <session-id>
```

The session is signed out at once. If it was a CLI login, that CLI's token
stops working, wherever it was copied. A browser session is asked to sign in
again.

The command refuses the session this CLI is logged in with, since that would
leave a login on this machine the server no longer accepts. Run
`volcano logout` to end it instead.

## Signing out every other session

```bash
volcano account sessions revoke --all-others
```

This signs out every session except this CLI's own: the dashboard in every
browser, and every other CLI login. It asks before doing so. Pass `--yes` to
skip the question, which a script has to do, since nothing can answer it there.

A login made with a token (`volcano login --token`, or a token created in the
dashboard) has no session of its own, so from it `--all-others` signs out every
session. The command says which it will do before it asks.

Signing out sessions revokes only the tokens exchanged from them. A token not
tied to a session, such as one passed to `volcano login --token` or created in
the dashboard, keeps working. Revoke the one this CLI uses with
`volcano logout`. Revoking your other platform tokens from the CLI is not
available yet. The command reminds you of this when it finishes.

Use it when a token or a device may be in the wrong hands. Afterwards, every
browser asks you to sign in again; run `volcano login` on each machine you still
use.
