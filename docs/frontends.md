---
title: "Frontends"
description: "A static or server-rendered site served by Volcano, optionally on your own custom domain and serving functions under its own paths."
---

## What it is

A deployed frontend (static or server-rendered site) served by Volcano,
optionally reachable at your own custom domain.

## How it relates

- Belongs to a **project**.
- Consumes **variables** at build/runtime.
- Can have one **custom domain** (BYOC) attached, at or below a domain your
  account has [verified](domains.md). A second, different domain is rejected
  with `409`; serve another hostname from a separate frontend.
- Can forward paths to public [functions](functions.md) through
  **function routes**.
- Frontend custom domains and function routes can also be declared in the
  [declarative config](project-configuration.md).

Frontend commands live under the `cloud` group.

## CLI operations

| Operation | Command |
|---|---|
| Deploy | `volcano cloud frontends deploy …` |
| Redeploy | `volcano cloud frontends redeploy …` |
| List | `volcano cloud frontends list` |
| Get | `volcano cloud frontends get <name>` |
| Delete | `volcano cloud frontends delete <name>` |
| Logs | `volcano cloud frontends logs <name>` |
| Custom domains | `volcano cloud frontends domain create\|list\|get\|delete …` |
| Function routes | `volcano cloud frontends routes list\|create\|update\|delete …` |

Cloud frontend deploys and redeploys use latest-wins queueing. A new request replaces
older queued work while the current deployment finishes. Delete supersedes
queued deploys and blocks later deploys until deletion finishes.

Use `--variable-scope all` to expose all project variables. Use
`--variable-scope scoped` with a repeatable `--variable NAME` flag to expose
selected variables. A scoped deploy with no `--variable` flags exposes no
project variables. Omitting both flags preserves the current selection when the
frontend exists.

`volcano cloud config deploy` never creates a frontend; an entry for one that
does not exist yet is skipped. Deploy a new frontend with the variables its
manifest entry selects, then run `volcano cloud config deploy`.

The selected variables other than `NEXT_PUBLIC_*` must fit the frontend's
4,096-byte runtime environment, of which Volcano uses about 900 bytes. See
[Environment size](/platform/frontends/deploy#environment-size).

`deploy` archives `--path` (default `.`) and leaves out `node_modules`, `.next`,
`dist`, `build`, `.env*.local`, `.git`, editor files, `*.log`, and anything in
the root `.gitignore`. Symbolic links are skipped with a warning, and more than
10,000 files fails the deploy. When the app uses `workspace:` dependencies,
`deploy` archives the workspace root instead, so these rules and the file limit
apply to the whole workspace; pass `--app-root` to archive only `--path`.

## Examples

```bash
# Deploy / redeploy a frontend
volcano cloud frontends deploy
volcano cloud frontends deploy --variable-scope scoped --variable API_URL --variable API_KEY
volcano cloud frontends redeploy my-site

# Inspect
volcano cloud frontends list
volcano cloud frontends logs my-site

# Attach and check a custom domain
volcano cloud frontends domain create my-site --domain app.example.com \
  --cert ./fullchain-leaf.pem --key ./privkey.pem
volcano cloud frontends domain get my-site
```

Attaching a custom domain needs a [verified domain](domains.md) above it. When
none exists, the command fails and prints the TXT record to publish.

The domain command shows a DNS routing target hostname. Configure a CNAME only if
your DNS provider confirms that your domain is not a zone apex. At an apex, use
a provider-supported ALIAS, ANAME, or CNAME-flattening record with that target.

## Function routes

A function route sends every request under a path of the frontend, such as
`/api/session`, to an HTTP-mode function. The browser stays on the frontend's
origin, so the function can keep a session in cookies.

```bash
# Forward /api/session to the session function; it sees /me for /api/session/me
volcano cloud frontends routes create web --path /api/session --function session --strip-prefix

# List them in match order, with each target's visibility
volcano cloud frontends routes list web

# Point the route at another function, or stop stripping the prefix
volcano cloud frontends routes update web /api/session --function session-v2
volcano cloud frontends routes update web /api/session --strip-prefix=false

# Stop forwarding the path
volcano cloud frontends routes delete web /api/session
```

`update` and `delete` take the route's path prefix or its ID. `update` keeps
whatever you leave out. `frontends get` lists the routes too:

```text
Function routes:
  /api/session -> session (public, strip prefix)
```

The target must be a `public` standard function with `invocation_mode: http`.
A route reaches it with no Volcano credential, so anyone who can load the
frontend can call it, and the function must authenticate its callers itself.
Volcano refuses a route to a `private` or `authenticated` function, and refuses
to make a routed function non-public until its routes are deleted; the CLI
prints the command that fixes each. A frontend can have up to 64 routes, and
the longest matching prefix wins.

A path prefix starts with `/`, does not end with `/`, has no `?`, `#`, or `\`,
and is 2 to 512 characters long. The CLI checks this before calling Volcano.

Declare `function_routes` in [`volcano-config.yaml`](project-configuration.md)
to manage the whole set at once. That is also how a local project gets routes:
local development has no `frontends` commands, so `volcano frontends routes`
refuses and points to `volcano cloud frontends routes`. See
[Frontend Function routes](/platform/frontends/function-routes) for how
requests are matched and how to keep a session in cookies.
