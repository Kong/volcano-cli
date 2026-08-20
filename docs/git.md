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
| Show the connection | `volcano git status` |
| Connect | `volcano git connect [git-url]` |
| Disconnect | `volcano git disconnect` |

## Seeing what is connected

```bash
volcano git status
```

This reports the project's own binding — the repository, the branch a push has
to land on, the subdirectory it builds from, and what a push deploys. It does
not contact GitHub, so it tells you what the platform recorded rather than
whether that recording still works.

A project with nothing connected is not an error: it says so and exits 0, so the
command is usable in a conditional. A project that does not exist — a deleted
one, or a `VOLCANO_PROJECT_ID` naming nothing — is an error, and is reported as
one rather than as an unbound project.

## Connecting

With no argument the repository is read from this directory's Git remotes — the
only remote, or `origin` when there are several. A remote with a separate
`pushurl` names two repositories, and the push target is the one bound, since a
push is what deploys; the CLI says so when the two differ. A remote configured
with *several* push URLs has no single repository to connect — a push reaches
all of them — so it is refused, and you name the repository yourself:

```bash
volcano git connect
```

Name a repository explicitly, or pick a different remote — the two cannot be
combined, since both say where the repository comes from:

```bash
volcano git connect https://github.com/octo/storefront.git
volcano git connect --remote upstream
```

For a monorepo, say which subdirectory the project builds from. It has to be a
path inside the repository — an absolute path, or one climbing out with `..`, is
refused rather than accepted and silently built from nothing:

```bash
volcano git connect --root-directory apps/api
```

Connecting is idempotent from your side: running it again on a project already
bound to the same repository reports that nothing changed. It does rewrite the
binding, which is how a project whose connected GitHub account was revoked
starts using a working one again. A repository renamed on
GitHub is still the same repository — the binding follows its id, not its name —
so re-running connect simply refreshes the name. Re-running it with a
different `--root-directory` edits that, which is the only way to change it
after the first connect. Pointing it at a *different* repository asks for
confirmation first, because pushes to the current one stop deploying. `--yes`
skips the prompts.

In a script or a CI job there is nobody to prompt, so a command that would ask
fails and says to pass `--yes` rather than cancelling silently.

After connecting, start deploying by pushing yourself. Only a push to the
repository's GitHub default branch deploys — `volcano git connect` prints which
branch that is, as `Production branch`. Pushing any other branch is safe and
deploys nothing.

```bash
git push origin main   # or whatever the CLI reported as the production branch
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
App settings, so `volcano git connect` reports that the integration is not
configured. Run these commands against the cloud API.

`volcano git disconnect` reads only the project's own binding, which does not
touch GitHub, so it reports that the project has nothing connected instead.
