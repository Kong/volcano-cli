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

The CLI ends up signed in as the account the browser uses. If you were already
signed in to Volcano in that browser, signup does not create an account: it
signs the CLI in to that account. When the login finishes, the CLI prints
`Logged in as <email>` with the account it signed in, which can differ from
the email you gave signup. If the CLI cannot read the account, it prints
`Logged in successfully` instead.

Already have an account? Authenticate with `volcano login`:

```bash
# Browser-based login (default); prints "Logged in as <email>"
volcano login

# Token-based login (for CI/CD)
volcano login --token pk-xxxxxxxxxx

# With the code in the dashboard's prompt
volcano login --device-code <code>

# Or skip login entirely with an environment variable
export VOLCANO_TOKEN=pk-xxxxxxxxxx
```

## Account tokens and project access tokens

The CLI accepts three kinds of credential:

| Credential | Reaches | Get one with |
|---|---|---|
| Account token (`pk-`) | Every project you own | `volcano login` |
| Project access token (`pt-`) | One project | `volcano cloud access-tokens create` |
| Delegated grant (`pg-`) | One project, only the commands you consented to | An app you consent to |

Account tokens and project access tokens work with `volcano login --token` and
with `VOLCANO_TOKEN`. A delegated grant is meant for `VOLCANO_TOKEN`, not for
`volcano login`; see [Delegated grants](#delegated-grants).

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

### Delegated grants

A delegated grant (`pg-`) is what a Volcano app uses to act on a project you
consented to. Like a project access token, it reaches one project; Volcano
also limits it to the commands you consented to. Pass it in `VOLCANO_TOKEN`.
A grant that has used up its changes fails with exit code `6`. See
[scripting.md](scripting.md#exit-codes).

`volcano login --token` does not refuse a grant. It checks the grant with
Volcano the way it checks an account token, and Volcano refuses a grant on
most routes. Without `--project`, or with a project name, the check fails and
login saves nothing. With `--project` set to the ID of the grant's own
project, the check passes if the grant may read the project, and login saves
the grant to `~/.volcano/config.json`. A saved grant stops working when the
app's run ends, so keep it in `VOLCANO_TOKEN` instead.

## Log out

Log out at any time:

```bash
volcano logout
```

This removes local credentials, the selected project, and function aliases,
but does not revoke the token. Revoke it in the Volcano dashboard to fully cut
off access.
