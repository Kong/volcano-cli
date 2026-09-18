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
- Can have one or more **custom domains** (BYOC) attached.
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
