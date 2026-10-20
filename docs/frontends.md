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
- Can have several **custom domains**, as many as the plan allows, each at or
  below a domain your account has [verified](domains.md). Each one serves the
  frontend or redirects to another of its domains.
- Can forward paths to public [functions](functions.md) through
  **function routes**.
- Frontend custom domains and function routes can also be declared in the
  [declarative config](project-configuration.md).

`volcano frontends …` targets local development and `volcano cloud frontends …`
targets the cloud project. The local server builds and serves each frontend at
the `Site URL` that `frontends get` prints. A local deploy prints that URL too,
except for a new frontend, which has a URL only once its first build finishes.
Custom domains are a cloud command.

## CLI operations

| Operation | Local | Cloud |
|---|---|---|
| Deploy | `volcano frontends deploy …` | `volcano cloud frontends deploy …` |
| Redeploy | `volcano frontends redeploy …` | `volcano cloud frontends redeploy …` |
| List | `volcano frontends list` | `volcano cloud frontends list` |
| Get | `volcano frontends get <name>` | `volcano cloud frontends get <name>` |
| Delete | `volcano frontends delete <name>` | `volcano cloud frontends delete <name>` |
| Logs | `volcano frontends logs <name>` | `volcano cloud frontends logs <name>` |
| Custom domains | — | `volcano cloud frontends domains list\|get\|add\|update\|remove …` |
| Function routes | `volcano frontends routes list\|create\|update\|delete …` | `volcano cloud frontends routes list\|create\|update\|delete …` |

Cloud frontend deploys and redeploys use latest-wins queueing. A new request replaces
older queued work while the current deployment finishes. Delete supersedes
queued deploys and blocks later deploys until deletion finishes.

Use `--variable-scope all` to expose all project variables. Use
`--variable-scope scoped` with a repeatable `--variable NAME` flag to expose
selected variables. A scoped deploy with no `--variable` flags exposes no
project variables. Omitting both flags preserves the current selection when the
frontend exists.

Builds never see variables named `VOLCANO_*`; the server runtime still gets
them. Browser code reads values Next.js inlines at build time, so give it
`NEXT_PUBLIC_*` copies, such as `NEXT_PUBLIC_VOLCANO_ANON_KEY`. See
[Variables](/platform/frontends/overview#variables).

`volcano cloud config deploy` never creates a frontend; an entry for one that
does not exist yet is skipped. Deploy a new frontend with the variables its
manifest entry selects, then run `volcano cloud config deploy`.
[`volcano deploy`](deploy.md) does both for every frontend listed in
`.volcano/source-layout.json`.

The selected variables other than `NEXT_PUBLIC_*` must fit the frontend's
4,096-byte runtime environment, of which Volcano uses about 900 bytes. See
[Environment size](/platform/frontends/deploy#environment-size).

`deploy` archives `--path` (default `.`) and leaves out `node_modules`, `.next`,
`dist`, `build`, `.env*.local`, the Volcano env files (`volcano/*.env` and
`volcano.env`), `.git`, editor files, `*.log`, and anything in the root
`.gitignore`. Symbolic links are skipped with a warning, and more than
10,000 files fails the deploy. When the app uses `workspace:` dependencies,
`deploy` archives the workspace root instead, so these rules and the file limit
apply to the whole workspace; pass `--app-root` to archive only `--path`.

## Examples

```bash
# Deploy a frontend to local development, then print its local URL once built
volcano frontends deploy --name web --path ./web
volcano frontends get web

# Deploy / redeploy a frontend in the cloud
volcano cloud frontends deploy
volcano cloud frontends deploy --variable-scope scoped --variable API_URL --variable API_KEY
volcano cloud frontends redeploy my-site

# Inspect
volcano cloud frontends list
volcano cloud frontends logs my-site
```

## Custom domains

```bash
# Serve the frontend on www, with a certificate Volcano issues
volcano cloud frontends domains add my-site www.example.com

# Once www is active, send the apex to it, keeping the path and query string
volcano cloud frontends domains add my-site example.com --redirect-to www.example.com

# Or serve your own certificate
volcano cloud frontends domains add my-site app.example.com \
  --cert ./fullchain-leaf.pem --key ./privkey.pem

volcano cloud frontends domains list my-site
volcano cloud frontends domains get my-site example.com
```

```text
Domain             TLS       Status    Created   Redirects to
www.example.com    managed   active    2m ago    -
example.com        managed   active    1m ago    www.example.com (308)
```

Without `--cert` and `--key`, Volcano issues and renews the certificate once you
publish the DNS records that `add` and `get` print. A BYOC domain needs a
[verified domain](domains.md) above it first; when none exists, `add` fails and
prints the TXT record to publish.

`get` shows the DNS routing target hostname. Point every domain of the frontend
at it, including the ones that redirect: the browser connects over HTTPS before
it reads the redirect. Configure a CNAME only if your DNS provider confirms that
your domain is not a zone apex. At an apex, use a provider-supported ALIAS,
ANAME, or CNAME-flattening record with that target.

### Redirects

`--redirect-status` takes 301, 302, 307, or 308, and defaults to 308. 301 and
308 are permanent and browsers cache them; use 302 while you test. A redirect
must point at an `active` domain of the same frontend that serves it, not at
another redirect or a domain still waiting for DNS or its certificate. If the
target stops serving, the redirect domain serves the frontend until the target
is active again.

```bash
# Change the status, or make the domain serve the frontend again
volcano cloud frontends domains update my-site example.com --redirect-to www.example.com --redirect-status 302
volcano cloud frontends domains update my-site example.com --no-redirect

volcano cloud frontends domains remove my-site example.com
```

A domain that others redirect to cannot be removed or become a redirect itself;
Volcano refuses with `409` and names them. Retarget or remove those first. To
swap which domain serves in one step, declare both in `custom_domains` in the
[declarative config](project-configuration.md) and run
`volcano cloud config deploy`.

Adding a domain past the plan's limit per frontend fails with `403`. On HOBBY,
managed certificates are also limited to one per account, so a second managed
domain fails with `403` on any frontend. See
[Plans and limits](/platform/guides/plans-and-limits#frontends).

Several domains per frontend are rolling out. Until they reach your project, a
second domain on a frontend is refused with `409`, and an older server answers
the `domains` commands with `404`. The `volcano cloud frontends domain
create|list|get|delete` commands work on every server and act on the
frontend's oldest domain that serves it.

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

The same commands without `cloud` manage routes on a local frontend. Declare
`function_routes` in [`volcano-config.yaml`](project-configuration.md) to manage
the whole set at once. See
[Frontend Function routes](/platform/frontends/function-routes) for how
requests are matched and how to keep a session in cookies.
