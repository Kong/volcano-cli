---
title: "Git connection"
description: "Bind a project to a GitHub repository so that pushing to its default branch deploys."
---

## What it is

A binding between a **project** and a GitHub repository. Once connected, a push
to the repository's default branch deploys the project — no CLI invocation
needed.

Connecting only records the binding. Volcano never creates push credentials,
never pushes on your behalf, and never writes a token into your `.git/config`.
Pushing stays your own `git push`, with the credentials already on your machine.

## How it relates

- Belongs to a **project**; one project binds to one repository.
- Requires a **GitHub account connected to your Volcano account**, and the
  Volcano GitHub App installed with access to the repository. Both are set up in
  the dashboard, not from the CLI.
- What a push actually deploys is governed by the project's Git deploy settings,
  reported after a successful connect.

## CLI operations

| Operation | Command |
|---|---|
| Connect | `volcano git connect [git-url]` |
| Disconnect | `volcano git disconnect` |

## Connecting

With no argument the repository is read from this directory's Git remotes — the
only remote, or `origin` when there are several:

```bash
volcano git connect
```

Name a repository explicitly, or pick a different remote:

```bash
volcano git connect https://github.com/octo/storefront.git
volcano git connect --remote upstream
```

For a monorepo, say which subdirectory the project builds from:

```bash
volcano git connect --root-directory apps/api
```

Connecting is idempotent: running it again on a project already bound to the
same repository reports the binding and changes nothing. Pointing it at a
*different* repository asks for confirmation first, because pushes to the
current one stop deploying. `--yes` skips the prompts.

After connecting, start deploying by pushing yourself:

```bash
git push -u origin main
```

## Disconnecting

```bash
volcano git disconnect
```

Only the binding is removed. The repository is untouched, and so is the GitHub
App's access to it — pushes simply stop deploying.

## Prerequisites the CLI cannot set up

Connecting a GitHub account is a browser redirect bound to a short-lived cookie,
so it has to happen in the dashboard. If no account is connected, or the Volcano
GitHub App cannot see the repository, the CLI says so and prints the dashboard
URL to fix it at.

The App being installed for **selected repositories** rather than all of them is
the usual cause of a repository the CLI can otherwise see in your remote but
cannot connect.

## Local mode

Git connections are a cloud-only feature: the local stack ships without GitHub
App settings, so these commands report that the integration is not configured.
Run them against the cloud API.
