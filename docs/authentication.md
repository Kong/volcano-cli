---
title: "Authentication"
description: "Sign up, log in, and manage your Volcano credentials from the CLI."
---

To sign in, create a project, and set up a directory for it in one step, use
`volcano connect`. See [connect.md](connect.md).

Local development needs no account: `volcano init`, `volcano start`, and the
local deploy commands work without signing in. When you first run a command
that needs an account, such as a `volcano cloud` command, the CLI asks whether
you already have one, then runs browser login or signup and carries on with
the command. It asks only when stdin and stderr are both terminals and none of
`CI`, `VOLCANO_NONINTERACTIVE`, or `TERM=dumb` is set. Otherwise, as in most
scripts and coding agents, the command fails with exit code `2` instead. See
[scripting.md](scripting.md).

New to Volcano? Create an account from the CLI:

```bash
volcano signup
```

`volcano signup` prefills your email from `git config --global user.email`
when available (press Enter to accept, or type a different address), then
opens Volcano's web signup flow in your browser. Once you finish in the
browser, the CLI completes the device-authorization handshake and saves your
credentials to `~/.volcano/config.json`, so a single command signs you up
and logs you in.

Already have an account? Authenticate with `volcano login`:

```bash
# Browser-based login (default)
volcano login

# Token-based login (for CI/CD)
volcano login --token pk-xxxxxxxxxx

# Or skip login entirely with an environment variable
export VOLCANO_TOKEN=pk-xxxxxxxxxx
```

## Account tokens and project access tokens

The CLI accepts three kinds of credential:

| Credential | Reaches | Get one with |
|---|---|---|
| Account token (`pk-`) | Every project you own | `volcano login` |
| Project access token (`pt-`) | One project | `volcano cloud access-tokens create` |
| One-time token (`vbt_`) | Exchanged for an account token on first use | The prompt on the dashboard's Projects page |

Account and project access tokens work with `volcano login --token` and with
`VOLCANO_TOKEN`. A one-time token works only with `--token` on `volcano login`
or `volcano connect`: it can be used once, within 15 minutes, and the CLI saves
the account token it gets in exchange. It saves that token before any other
check, so a failure after the exchange, such as a wrong `--project`, leaves
you signed in. See
[One-time tokens from the dashboard](connect.md#one-time-tokens-from-the-dashboard).

A project access token is the one to give CI: it is scoped to a single project, carries
`full` or `read_only` access, and can be revoked on its own. Because it cannot
list projects, tell the CLI which project it belongs to, by ID:

```bash
volcano login --token pt-xxxxxxxxxx --project <project-id>

# Or, without logging in
export VOLCANO_TOKEN=pt-xxxxxxxxxx
export VOLCANO_PROJECT_ID=<project-id>
```

`--project` also selects the project for an account token, and there it takes a
name as well as an ID, the same as `volcano use`:

```bash
volcano login --token pk-xxxxxxxxxx --project my-app
```

Commands that reach past one project — `volcano projects list`, `volcano
projects create`, `volcano projects rename`, `volcano projects delete`,
selecting a project by name — need an account token, and say so when run with a
project access token. See
[access-tokens.md](access-tokens.md).

Log out at any time:

```bash
volcano logout
```

This deletes local credentials but does not revoke the token. Revoke it in the
Volcano dashboard to fully cut off access.
