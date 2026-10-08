---
title: "Frontends"
description: "A deployed frontend (static or server-rendered site) served by Volcano, optionally reachable at your own custom domain."
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
- Frontend custom domains can also be declared in the
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
