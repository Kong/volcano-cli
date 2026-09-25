---
title: "Authentication"
description: "Sign up, log in, and manage your Volcano credentials from the CLI."
---

New to Volcano? Create an account from the CLI:

```bash
volcano signup
```

`volcano signup` prefills your email from `git config --global user.email`
when available (press Enter to accept, or type a different address), then
opens Volcano's web signup flow in your browser. Once you finish in the
browser, the CLI completes the device-authorization handshake and saves your
credentials to `~/.volcano/config.json`, so a single command signs you up
and logs you in. By default, the CLI uses `https://api.volcano.dev` and opens
`https://volcano.dev` for signup.

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

The CLI accepts two kinds of credential:

| Credential | Reaches | Get one with |
|---|---|---|
| Account token (`pk-`) | Every project you own | `volcano login` |
| Project access token (`pt-`) | One project | `volcano cloud access-tokens create` |

Both work with `volcano login --token` and with `VOLCANO_TOKEN`. A project
access token is the one to give CI: it is scoped to a single project, carries
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
