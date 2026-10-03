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
- Can have one or more **custom domains** (BYOC) attached.
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
volcano cloud frontends domain create my-site --domain app.example.com
volcano cloud frontends domain get my-site
```

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
to make a routed function non-public until its routes are deleted. A frontend
can have up to 64 routes, and the longest matching prefix wins.

Declare `function_routes` in [`volcano-config.yaml`](project-configuration.md)
to manage the whole set at once. See
[Frontend Function routes](/platform/frontends/function-routes) for how
requests are matched and how to keep a session in cookies.
