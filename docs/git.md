---
title: "Export a project to GitHub"
description: "Put a Volcano project in a GitHub repository and push to it, so that every push deploys."
---

## What it is

`volcano git export` puts the current project in a GitHub repository and pushes
this checkout into it. From then on a push to the project's production branch
deploys — no CLI invocation needed.

```bash
volcano git export --repo storefront --branch main
```

Volcano never creates push credentials and never writes a token into your
`.git/config`. Every push runs as your own `git push`, with the credentials
already on your machine — including the first one this command runs for you.

## How it relates

- Belongs to a **project** — the one `volcano use` selected. One project binds to
  one repository.
- Requires a **GitHub account connected to your Volcano account**, and the
  Volcano GitHub App installed on the account the repository lives under. Both
  are set up in the dashboard, not from the CLI.
- What a push actually deploys is governed by the project's Git deploy settings,
  reported when the command finishes.

## CLI operations

| Operation | Command |
|---|---|
| Show the connection | `volcano git status` |
| Export to a repository | `volcano git export --repo <name> --branch <branch>` |

Disconnecting a repository, and pointing a project at a different one, are
dashboard flows.

## Nothing is inferred

Both the repository and the branch are stated, never guessed:

- **`--repo`** — `name`, or `owner/name` for an organization. The directory this
  runs in says nothing about what the repository should be called, and a
  repository is the one thing here Volcano cannot delete afterwards.
- **`--branch`** — the branch to push, which becomes the branch that deploys. The
  CLI runs from any terminal and any checkout, so whichever branch happens to be
  checked out is not a statement about what should be deployed. It does **not**
  have to be the branch you are on: git pushes any local branch, so there is
  nothing to switch first.

The project comes from `volcano use`, and is named back to you in the report.

## The three shapes

Which one applies is a fact about GitHub, so the command works it out rather than
asking:

| The repository | What happens |
|---|---|
| Does not exist | Created empty and private, bound, then pushed. |
| Exists, no branches | Bound, its production branch pointed at your branch, then pushed. |
| Exists, has history | Bound, and your branch pushed to **`volcano/export`** — not to the production branch. |

The third case is the one to understand. Your history and the repository's are
unrelated, so a push to the production branch would be refused and forcing it
would discard what is already there. Instead the project lands on a branch of its
own and the command hands back a compare URL:

```text
✓ Connected octo/storefront
✓ Pushed to volcano/export on octo/storefront

Warning: octo/storefront already has its own history, so the project was pushed
to volcano/export rather than merged into main. Nothing deploys until that merge
lands.
Open a pull request:
  https://github.com/octo/storefront/compare/main...volcano/export?expand=1
```

**Nothing deploys until that pull request merges.** That is deliberate: the
overlap between the two trees is for a human to resolve, and GitHub's review
tooling is where that is done. Exporting again updates the same branch and the
same pull request.

## Before anything irreversible

Volcano cannot delete a GitHub repository, so `git export` asks before it creates
one, and everything it can check it checks first:

- a GitHub account is connected to your Volcano account;
- the App is installed on the account in `--repo owner/name`;
- the project has no repository already;
- `--repo` is a name GitHub will create as written, rather than one it would
  silently rewrite;
- the branch exists in this checkout;
- the remote name is free, and one git accepts.

Pass `--yes` to skip the prompt in a script or an agent. It skips the question,
not the checks.

## Options

```bash
volcano git export --repo acme/storefront --branch main --public
volcano git export --repo storefront --branch main --root-directory apps/api
volcano git export --repo storefront --branch main --no-push
```

| Flag | Effect |
|---|---|
| `--repo` | **Required.** `name`, or `owner/name` to use an organization. The App must be installed on that account, and your own GitHub permissions still apply. |
| `--branch` | **Required.** The branch to push, and the branch that deploys. |
| `--public` | Create a public repository. The default is private, since the next step is pushing your source into it. Ignored for a repository that already exists. |
| `--description` | Repository description shown on GitHub. |
| `--root-directory` | Monorepo subdirectory the project builds from, as a relative path inside the repository. |
| `--remote` | Name for the Git remote. Defaults to `origin`. It has to be a name git accepts, and an existing remote of that name is never taken over. |
| `--ssh` | Record the remote with its ssh URL. The default is https, which git rewrites for you if you have `url.<base>.insteadOf` configured. |
| `--no-push` | Create or bind the repository without touching this checkout. |
| `--yes` | Skip the confirmation prompt. |

### Exporting without pushing

`--no-push` leaves the working directory alone — no remote is added and nothing
is pushed. Use it when the source is somewhere else. The command prints the two
steps left:

```bash
git remote add origin https://github.com/octo/storefront.git
git push --set-upstream origin main
```

### When a later `git push` would go elsewhere

`--set-upstream` loses to `branch.<name>.pushRemote` and `remote.pushDefault`,
which git consults first. If either is set in this checkout, the export's own
push lands but a later bare `git push` does not — so the command says so, names
the setting, and prints the push that does reach the repository.

### When the App cannot see a new repository

If the Volcano GitHub App is installed for **selected repositories** rather than
all of them, it may not cover a repository created after the fact. The repository
and the binding are both fine; no push would deploy. `git export` reports this
instead of pushing, and prints where to grant access. Once granted, push with the
command it printed — there is nothing to redo.

Installing the App with access to all repositories on the account avoids this.

## Seeing what is connected

```bash
volcano git status
```

This reports the project's own binding — the repository, the branch a push has to
land on, the subdirectory it builds from, and what a push deploys. It does not
contact GitHub, so it tells you what the platform recorded rather than whether
that recording still works.

A project with nothing connected is not an error: it says so and exits 0, so the
command is usable in a conditional. A project that does not exist — a deleted
one, or a `VOLCANO_PROJECT_ID` naming nothing — is an error, and is reported as
one rather than as an unbound project.

## If it fails

A failure saying a repository **may have been created** means exactly that: it
may be on GitHub. Do not re-run with a different name — that leaves you owning
two. Check the account, and connect the repository that is there from the
dashboard. This covers an export that was interrupted, or that reached GitHub
while the answer never made it back.

A failed push is the mildest case: the repository and the binding are both in
place, the remote is recorded, and the push is yours to retry.

## Prerequisites the CLI cannot set up

Connecting a GitHub account is a browser redirect bound to a short-lived cookie,
so it has to happen in the dashboard. `git export` checks for it before it
creates anything, and prints the dashboard URL when no account is connected.

The App also has to be installed on the account you export to. When the account
you named is not one of them, the command says which accounts it *is* installed
on.

## Local mode

Git connections are a cloud-only feature: the local stack ships without GitHub
App settings, so `volcano git export` reports that the integration is not
configured. Run it against the cloud API.
