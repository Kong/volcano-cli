---
title: "Authentication"
description: "Sign up, log in, and manage your Volcano credentials from the CLI."
---

To sign in, create a project, and set up a directory for it in one step, use
`volcano connect`. See [connect.md](connect.md).

Local development needs no account: `volcano init`, `volcano start`, and the
local deploy commands work without signing in. A command that needs an
account, such as a `volcano cloud` command, fails with exit code `2` until you
run `volcano login`, or `volcano signup` for a new account. See
[scripting.md](scripting.md).

New to Volcano? Create an account from the CLI:

```bash
volcano signup
```

`volcano signup` prefills your email from `git config --global user.email`
when it is a usable address (press Enter to accept, or type a different
address). Private relay addresses, such as GitHub or GitLab `noreply`
addresses, and local or test addresses are not prefilled. The command then
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

# With the code in the dashboard's prompt
volcano login --device-code <code>

# Or skip login entirely with an environment variable
export VOLCANO_TOKEN=pk-xxxxxxxxxx
```

## Account tokens and project access tokens

The CLI accepts two kinds of credential:

| Credential | Reaches | Get one with |
|---|---|---|
| Account token (`pk-`) | Every project you own | `volcano login` |
| Project access token (`pt-`) | One project | `volcano cloud access-tokens create` |

Both work with `volcano login --token` and with `VOLCANO_TOKEN`.

The prompt on the dashboard's Projects page holds a device code instead of a
token. It works with `--device-code` on `volcano login` or `volcano connect`:
it can be used once, within 10 minutes, and the CLI saves the account token it
signs in with. It saves that token before any other check, so a failure after
sign-in, such as a wrong `--project`, leaves you signed in. See
[Device codes from the dashboard](connect.md#device-codes-from-the-dashboard).

A project access token is the one to give CI: it is scoped to a single project, carries
`full` or `read_only` access, and can be revoked on its own. Because it cannot
list projects, tell the CLI which project it belongs to, by ID:

```bash
volcano login --token pt-xxxxxxxxxx --project <project-id>

# Or keep the token in VOLCANO_TOKEN and save only the project
export VOLCANO_TOKEN=pt-xxxxxxxxxx
volcano use <project-id>
```

`VOLCANO_PROJECT_ID` still selects the project, but it is deprecated and will
be removed in a future release. A command that uses it prints a warning on
stderr.

`--project` also selects the project for an account token, and there it takes a
name as well as an ID, the same as `volcano use`:

```bash
volcano login --token pk-xxxxxxxxxx --project my-app
```

Inside a [linked directory](connect.md#linked-directories), `--project` updates
the link, the same as `volcano use`. For a project access token, login also
saves the project to `~/.volcano/config.json`. If the link named another project, login
warns and prints the command that links it back. If the deprecated
`VOLCANO_PROJECT_ID` names a different project there, login fails and leaves
the link as it was.

Commands that reach past one project — `volcano projects list`, `volcano
projects create`, `volcano projects rename`, `volcano projects delete`,
selecting a project by name — need an account token, and say so when run with a
project access token. See
[access-tokens.md](access-tokens.md).

Log out at any time:

```bash
volcano logout
```

This ends the login on the server, then removes it from this machine. The
server revokes the saved token and the session it came from, so the token
stops working everywhere it was copied. The CLI then removes the token, the
selected project, and function aliases from `~/.volcano/config.json`. Your
other CLI logins and browser sessions stay signed in.

If the server can't be reached or fails, `volcano logout` says so, exits
non-zero, and keeps the login saved on this machine. Run `volcano logout` again
to end it, or end it from another login with
`volcano account sessions revoke <id>`. If the server
refuses to revoke the login, `volcano logout` removes it from this machine
anyway and exits non-zero. The token then stays valid until it expires. If it
was a project access token, revoke it with `volcano cloud access-tokens revoke`.
If it came from a sign-in, end that session from another login with
`volcano account sessions revoke <id>`; see [account.md](account.md). Ending a
token that isn't tied to a session isn't available from the CLI yet.

To remove the login from this machine without reaching the server, for example
while offline, run `volcano logout --no-revoke`. The token then stays valid
until it expires.

Local development keeps no sign-in session on the server, so there
`volcano logout` only removes the login from this machine.

Two credentials stay valid after `volcano logout`:

- A project access token (`pt-`) is removed from this machine but not revoked.
  Revoke it with `volcano cloud access-tokens revoke`. See
  [access-tokens.md](access-tokens.md).
- A token in `VOLCANO_TOKEN` was never saved, so `volcano logout` doesn't unset
  it, and commands keep using it. If it is the same token as the saved login,
  logout revokes it with the login, and the command tells you to unset it.
