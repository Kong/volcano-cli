---
title: "Git connection"
description: "Bind a project to a GitHub repository — one you already have, or one Volcano creates for you — so that pushing deploys."
---

## What it is

A binding between a **project** and a GitHub repository. Once connected, a push
to the production branch deploys the project — no CLI invocation needed.

Bring a repository you already have with `volcano git connect`, or have one
created for a project that has none with `volcano git create`.

Volcano never creates push credentials and never writes a token into your
`.git/config`. Every push runs as your own `git push`, with the credentials
already on your machine — including the first one `volcano git create` runs for
you.

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
| Create a repository and push to it | `volcano git create [name]` |
| Connect an existing repository | `volcano git connect [git-url]` |
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

## Creating a repository

For a project that has no repository yet — one scaffolded with `volcano init`, or
built by an agent — Volcano creates the repository and pushes this checkout into
it:

```bash
volcano git create
```

With no argument the repository takes this directory's name. It is created
**empty** and **private**: no initial commit, no README. The first commit is the
one you already have here, pushed as your own `git push`.

The command creates the repository, connects the project to it, adds a Git remote
(`origin`, or `--remote`), and pushes the branch you are on. Auto-deploy is on
from the moment a repository is connected, so that first push deploys — what it
deploys is reported when the command finishes.

**Nothing here can be undone from the CLI.** Volcano cannot delete a GitHub
repository, so `git create` asks before it creates anything, and everything it
can check it checks first: that the project has no repository, that this checkout
has a commit to push, and that the remote name is free. Pass `--yes` to skip the
prompt in a script or an agent.

### The branch that deploys

Only pushes to the project's **production branch** deploy. An empty repository
has no commits and therefore no real default branch, so `git create` sends the
branch you are standing on — the one it is about to push — rather than leaving
the platform to predict one. Name a different branch with `--branch`; it has to
exist in this checkout, because it is also the branch that gets pushed.

### Choosing the account and layout

```bash
volcano git create storefront --owner acme --public
volcano git create --root-directory apps/api
volcano git create --ssh
```

| Flag | Effect |
|---|---|
| `--owner` | GitHub account to create under. Omit for your own account. The Volcano App must be installed on it, and your own GitHub permissions still apply. |
| `--public` | Create a public repository. The default is private, since the next step is pushing your source into it. |
| `--description` | Repository description shown on GitHub. |
| `--root-directory` | Monorepo subdirectory the project builds from. Same rule as `git connect`: a relative path inside the repository. |
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
repository and the binding are both fine; no push would deploy. `git create`
reports this instead of pushing, and prints where to grant access. Once granted,
push with the command it printed — there is nothing to redo.

Installing the App with access to all repositories on the account avoids this
entirely.

### If it fails

A failure saying a repository **may have been created** means exactly that: it
may be on GitHub. Do not re-run with a different name — that leaves you owning
two. Check the account, then connect the one that is there:

```bash
volcano git connect https://github.com/octo/storefront.git
```

This covers a create that was interrupted, or that reached GitHub while the
answer never made it back. In both, the repository exists and nothing recorded
it.

A failed push is the mildest case: the repository and the binding are both in
place, the remote is recorded, and the push is yours to retry.

## Connecting

With no argument the repository is read from this directory's Git config. The
one git would push to wins — `git push` follows `branch.<name>.pushRemote`, then
`remote.pushDefault`, then `branch.<name>.remote` — because a push is what
deploys, so in a fork or triangular checkout the repository bound is the one you
push to rather than the one you fetch from. Failing all of that: the only
remote, or `origin` when there are several. The CLI says what it used whenever
the configuration, not the convention, decided it.

Those three keys hold either a remote name or a repository URL — `git push`
accepts both — so a URL is followed to the repository it names, even though no
remote in the checkout describes it, and even in a checkout with no remotes at
all. A value that is neither, such as a typo for a remote name, is refused rather
than quietly falling back to `origin`: falling back would bind a repository this
checkout never pushes to. A value that is set but empty, or padded with
whitespace, is refused rather than skipped: git uses these verbatim and fails on
them instead of falling through to the next key, so skipping one would bind
whatever the convention picked for a checkout that cannot push at all.
Credentials are never echoed back, since a CI rewrite routinely leaves a job
token in one of these values.

`--remote` and a repository URL both outrank the routing, and neither reads it at
all — so either one connects a checkout whose routing is broken, without having
to edit the Git config first.

`url.<base>.pushInsteadOf` and `url.<base>.insteadOf` are applied to such a URL
before it is resolved, the same way `git push` applies them — push rules first,
then fetch rules, the longest matching prefix winning and the first of two rules
sharing a prefix. So the repository bound is the one a push reaches rather than
the one the setting spells. A remote named in the usual way needs none of this:
`git remote -v` already reports the rewritten push URL.

A remote with a separate `pushurl` names two repositories, and the push target is
the one bound, since a push is what deploys; the CLI says so when the two differ.
A remote configured with *several* push URLs has no single repository to connect
— a push reaches all of them — so it is refused, and you name the repository
yourself:

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
refused rather than accepted and silently built from nothing. An empty value
resets it to the repository root; whitespace is refused, since that is a mistyped
value or an unset variable rather than a request to clear:

```bash
volcano git connect --root-directory apps/api
volcano git connect --root-directory ""     # back to the repository root
```

Connecting is idempotent: running it again on a project already bound to the
same repository reports that nothing changed and writes nothing. "The same" means
every part of the binding — the repository, the App installation, the root
directory, and the production branch — so a repository whose GitHub default
branch has moved is *not* unchanged, and re-running connect updates the branch a
push has to land on. A repository renamed on
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

A bare `git push` goes to the repository the binding was read from, since both
follow the same routing:

```bash
git push
```

If you name a remote instead, name the connected one. In a fork or triangular
checkout `git push origin` goes to the repository you *fetch* from, which is not
the one connected and deploys nothing — `volcano git status` reports which
repository is bound.

## Disconnecting

```bash
volcano git disconnect
```

Only the binding is removed. The repository is untouched, and so is the GitHub
App's access to it — pushes simply stop deploying. That holds for a repository
`git create` made too: disconnecting does not delete it, and nothing in the CLI
does.

## Prerequisites the CLI cannot set up

Connecting a GitHub account is a browser redirect bound to a short-lived cookie,
so it has to happen in the dashboard. If no account is connected, or the Volcano
GitHub App cannot see the repository, the CLI says so and prints the dashboard
URL to fix it at.

The App being installed for **selected repositories** rather than all of them is
the usual cause of a repository the CLI can otherwise see in your remote but
cannot connect.

`volcano git create` needs the App installed on the account it creates under. It
says which accounts it is installed on when the one you named is not among them.

## Local mode

Git connections are a cloud-only feature: the local stack ships without GitHub
App settings, so `volcano git connect` and `volcano git create` report that the
integration is not configured. Run these commands against the cloud API.

`volcano git disconnect` reads only the project's own binding, which does not
touch GitHub, so it reports that the project has nothing connected instead.
