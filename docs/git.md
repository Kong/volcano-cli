---
title: "Git repository"
description: "Create a GitHub repository for a Volcano project and push to it, so that every push deploys."
---

## What it is

A binding between a **project** and a GitHub repository. Once bound, a push to
the production branch deploys the project — no CLI invocation needed.

From the CLI you create that repository:

```bash
volcano git export
```

Volcano never creates push credentials and never writes a token into your
`.git/config`. Every push runs as your own `git push`, with the credentials
already on your machine — including the first one `volcano git export` runs for
you.

## How it relates

- Belongs to a **project**; one project binds to one repository.
- Requires a **GitHub account connected to your Volcano account**, and the
  Volcano GitHub App installed on the account the repository is created under.
  Both are set up in the dashboard, not from the CLI.
- What a push actually deploys is governed by the project's Git deploy settings,
  reported when the command finishes.

## CLI operations

| Operation | Command |
|---|---|
| Show the connection | `volcano git status` |
| Export to a new repository and push | `volcano git export [name]` |

Connecting a repository that already exists, and disconnecting one, are dashboard
flows.

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

## Creating a repository

For a project that has no repository yet — one scaffolded with `volcano init`, or
built by an agent — Volcano creates the repository and pushes this checkout into
it:

```bash
volcano git export
```

With no argument the repository takes this directory's name. It is created
**empty** and **private**: no initial commit, no README. The first commit is the
one you already have here, pushed as your own `git push`.

The command creates the repository, connects the project to it, adds a Git remote
(`origin`, or `--remote`), and pushes the branch you are on. Auto-deploy is on
from the moment a repository is connected, so that first push deploys — what it
deploys is reported when the command finishes.

**Nothing here can be undone from the CLI.** Volcano cannot delete a GitHub
repository, so `git export` asks before it creates anything, and everything it
can check it checks first:

- a GitHub account is connected to your Volcano account;
- the App is installed on the account you named with `--owner`;
- the project has no repository already;
- this checkout has a commit to push, on a branch git will accept;
- the remote name is free.

Pass `--yes` to skip the prompt in a script or an agent.

### The branch that deploys

Only pushes to the project's **production branch** deploy. An empty repository
has no commits and therefore no real default branch, so `git export` sends the
branch you are standing on — the one it is about to push — rather than leaving
the platform to predict one. Name a different branch with `--branch`; it has to
exist in this checkout, because it is also the branch that gets pushed.

### Choosing the account and layout

```bash
volcano git export storefront --owner acme --public
volcano git export --root-directory apps/api
volcano git export --ssh
```

| Flag | Effect |
|---|---|
| `--owner` | GitHub account to create under. Omit for your own account. The Volcano App must be installed on it, and your own GitHub permissions still apply. |
| `--public` | Create a public repository. The default is private, since the next step is pushing your source into it. |
| `--description` | Repository description shown on GitHub. |
| `--root-directory` | Monorepo subdirectory the project builds from, as a relative path inside the repository. |
| `--branch` | Branch to deploy from, and to push. Defaults to the branch this checkout is on. |
| `--remote` | Name for the new Git remote. Defaults to `origin`. It has to be a name git accepts, and an existing remote of that name is never taken over. |
| `--ssh` | Record the remote with its ssh URL. The default is https, which git rewrites for you if you have `url.<base>.insteadOf` configured. |
| `--no-push` | Create and connect the repository without touching this checkout. |

### Creating without pushing

`--no-push` leaves the working directory alone — no remote is added and nothing
is pushed. Use it when the source is somewhere else, or when there is no commit
yet. The command prints the two steps left:

```bash
git remote add origin https://github.com/octo/storefront.git
git push --set-upstream origin main
```

### When the App cannot see the new repository

If the Volcano GitHub App is installed for **selected repositories** rather than
all of them, it may not cover a repository created after the fact. The
repository and the binding are both fine; no push would deploy. `git export`
reports this instead of pushing, and prints where to grant access. Once granted,
push with the command it printed — there is nothing to redo.

Installing the App with access to all repositories on the account avoids this
entirely.

### If it fails

A failure saying a repository **may have been created** means exactly that: it
may be on GitHub. Do not re-run with a different name — that leaves you owning
two. Check the account, and connect the repository that is there from the
dashboard.

This covers a create that was interrupted, or that reached GitHub while the
answer never made it back. In both, the repository exists and nothing recorded
it.

A failed push is the mildest case: the repository and the binding are both in
place, the remote is recorded, and the push is yours to retry.

## Prerequisites the CLI cannot set up

Connecting a GitHub account is a browser redirect bound to a short-lived cookie,
so it has to happen in the dashboard. `volcano git export` checks for it before
it creates anything, and prints the dashboard URL when no account is connected.

The App also has to be installed on the account you create under. When the
account you named is not one of them, the command says which accounts it *is*
installed on.

## Local mode

Git connections are a cloud-only feature: the local stack ships without GitHub
App settings, so `volcano git export` reports that the integration is not
configured. Run it against the cloud API.
