---
title: "Git connection"
description: "Bind a project to a GitHub repository so that pushing to its production branch deploys."
---

## What it is

A binding between a **project** and a GitHub repository. Once connected, a push
to the repository's default branch, or to a branch you pin instead, deploys the
project — no CLI invocation needed.

Connecting only records the binding. Volcano never creates push credentials
and never writes a token into your `.git/config`. Pushing stays your own
`git push`, with the credentials already on your machine. The one exception is
`volcano git export`, which you run once to write the project's stored source
into an empty repository; Volcano pushes that first commit itself.

## How it relates

- Belongs to a **project**; one project binds to one repository.
- Requires a **GitHub account connected to your Volcano account**, and the
  Volcano GitHub App installed with access to the repository. Both are set up in
  the dashboard, not from the CLI.
- `connect` reads that account-wide GitHub connection, so it needs an account
  token (`pk-`) and says so when run with a project access token. Which
  repository a project deploys from is a person's choice.
- `root-directory` changes only the subdirectory the connected repository
  builds from, and works with a project access token, so a CI job or an agent
  can use it.
- `production-branch`, `auto-deploy`, `functions` and `frontend` each change
  one thing about what a push deploys, and also work with a project access
  token.
- What a push actually deploys is governed by the project's Git deploy settings,
  reported after a successful connect and listed by `volcano git status`. When
  a push deploys nothing, or no frontend, the report ends with the command that
  changes that.

## CLI operations

| Operation | Command |
|---|---|
| Show the connection | `volcano git status` |
| List the repositories you can connect | `volcano git repos` |
| Connect | `volcano git connect [git-url]` |
| Change the root directory | `volcano git root-directory <path>` |
| Change the branch that deploys | `volcano git production-branch <branch>` |
| Turn deploying on push on or off | `volcano git auto-deploy on\|off` |
| Choose whether functions deploy | `volcano git functions on\|off` |
| Choose the frontend a push deploys | `volcano git frontend <name> [--root <path>]` |
| Export the project's source to an empty repository | `volcano git export [--branch <branch>]` |
| Cancel an incomplete export | `volcano git export cancel` |
| Disconnect | `volcano git disconnect` |

## Seeing what is connected

```bash
volcano git status
```

This reports the project's own binding and every Git deploy setting: the
repository, the branch a push has to land on and whether it follows the
repository's default branch or is pinned, the subdirectory it builds from,
whether auto-deploy is on, whether functions deploy, and the frontend with the
directory it builds from. It also reports the project's source of truth:
Volcano, or the repository once an export has handed it over. It ends with what
a push deploys. It does
not contact GitHub, so it tells you what the platform recorded rather than
whether that recording still works.

A project with nothing connected is not an error: it says so and exits 0, so the
command is usable in a conditional. A project that does not exist — a deleted
one, or a deprecated `VOLCANO_PROJECT_ID` naming nothing — is an error, and is reported as
one rather than as an unbound project.

## Listing the repositories you can connect

```bash
volcano git repos
```

```text
GitHub account: octo

octo (user, all repositories)
  octo/storefront private, default branch main

acme (organization, selected repositories)
  acme/new-site public, empty

Connect one with volcano git connect https://github.com/<owner>/<repo>
```

This lists every repository your GitHub connection reaches, grouped by the
Volcano GitHub App installation that grants access. An installation scoped to
selected repositories lists only those. A repository marked `empty` has no
commits, so it can receive the project's source with `volcano git export`. Like
`connect`, it reads your account's GitHub connection, so it needs an account
token. `--json` prints each installation with its `repositories`.

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

A repository with no commits, such as one made to receive
`volcano git export`, has no default branch for pushes to follow, so name the
branch when you connect it. The CLI refuses to connect one without it:

```bash
volcano git connect https://github.com/octo/new-site --production-branch main
```

`--production-branch` sets the branch of a new binding only. On the repository
already connected, change it with `volcano git production-branch`. When
replacing a binding with a repository that has commits, the new binding starts
on that repository's default branch; connect without the flag, then pin
another.

Connecting is idempotent: running it again on a project already bound to the
same repository reports that nothing changed and writes nothing. "The same" means
every part of the binding — the repository, the App installation, the root
directory, and the production branch. For a project that follows the
repository's default branch, a default branch that has moved on GitHub is *not*
unchanged, and re-running connect moves the project to the new default. A branch
pinned with `volcano git production-branch` stays: re-running connect for the
same repository keeps it, even when it rebinds to refresh the installation or
the name. A repository renamed on GitHub is still the same repository — the
binding follows its id, not its name — so re-running connect simply refreshes
the name. Re-running it with a
different `--root-directory` edits that, as `volcano git root-directory` does. Pointing it at a *different* repository asks for
confirmation first, because pushes to the current one stop deploying. `--yes`
skips the prompts.

In a script or a CI job there is nobody to prompt, so a command that would ask
fails and says to pass `--yes` rather than cancelling silently.

After connecting, start deploying by pushing yourself. Only a push to the
production branch deploys. That is the repository's GitHub default branch
unless you pin another with `volcano git production-branch`, and `volcano git
connect` prints it as `Production branch`. Pushing any other branch is safe and
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

## Changing the root directory

For a monorepo, change the subdirectory the project builds from without
changing which repository is connected:

```bash
volcano git root-directory apps/web
volcano git root-directory --reset     # back to the repository root
```

The path is relative to the repository root. An absolute path, or one climbing
out with `..`, is refused. Volcano stores it in the form a deploy reads it, so
`./apps/web/` becomes `apps/web`.

This works with a project access token, so a CI job or an agent can move a
project within its repository. Connecting a different repository needs a
person's account token. With a project access token, `volcano git connect
--root-directory` for the repository already connected points to this command
instead.

Nothing is redeployed. The next deploy from the repository builds from the new
directory. Like `connect`, the command then says what a push to the production
branch deploys, which is nothing while auto-deploy is off.

A project with no repository connected has no root directory to change, so the
command says so and exits non-zero.

## Changing the branch that deploys

A project deploys from the repository's default branch on GitHub until you name
another one, and renaming the default branch on GitHub moves the project with
it. Name a branch to pin the project to it:

```bash
volcano git production-branch release
volcano git production-branch --follow-default     # follow the default branch again
```

A pinned branch stays put when the default branch is renamed, even when you
pinned the branch that is the default today. `volcano git status` says which
branch a project deploys from, and whether it follows the default or is
pinned.

The branch does not have to exist yet, so you can point a project at a branch
you are about to push. Volcano checks that it is a valid Git branch name and
nothing more. `--follow-default` reads the current default branch from GitHub,
so it fails for a repository with no commits yet. Push one first.

Re-running `volcano git connect` for the same repository keeps a pinned branch.
Connecting a different repository follows that repository's default branch:
set the branch again afterwards if you need another.

Nothing is deployed. Only pushes to the branch deploy from now on. This works
with a project access token.

## Turning auto-deploy and functions on or off

```bash
volcano git auto-deploy off     # pushes deploy nothing
volcano git auto-deploy on
volcano git functions off       # pushes stop deploying the repository's functions
volcano git functions on
```

While auto-deploy is off, a push deploys nothing, whatever else is selected.
Turning either setting off removes nothing: what is already deployed stays up.
The other deploy settings stay as they were, and the command says what a push
deploys from now on. Both work with a project access token.

## Choosing the frontend a push deploys

A connected repository deploys its functions on push. To deploy a frontend from
it too, name the frontend, and the directory it builds from when that isn't the
project's root directory:

```bash
volcano git frontend web --root apps/web
volcano git frontend --none     # deploy no frontend on push
```

`--root` is relative to the project's root directory (see `volcano git
root-directory`), not to the repository root. An absolute path, or one climbing
out with `..`, is refused. The frontend need not exist yet: the next push
deploys it under this name.

The other deploy settings, such as whether functions deploy, stay as they were.
This works with a project access token, so a CI job or an agent can set it.

Nothing is redeployed. While auto-deploy is on, the next push to the
production branch deploys the frontend. The command says what a push deploys
from now on, which is nothing while auto-deploy is off. `--none` stops
pushes from redeploying a frontend, but a frontend already deployed stays up.

The project needs a connected repository first, so on a project with nothing
connected the command says so and exits non-zero. The same goes for
`production-branch`, `auto-deploy` and `functions`.

## Exporting the project's source

A project you deployed from the CLI keeps its source in Volcano. To move it into
Git, create an empty GitHub repository (no README, license or other first
commit), connect it, and export. A repository with no commits has no default
branch, so name the branch pushes will deploy from when you connect it:

```bash
volcano git connect https://github.com/acme/storefront --production-branch main
volcano git export
```

Export writes the source of each function and frontend, taken from its latest
successful deployment, as the first commit on the production branch, and
pushes it. Anyone who can read the repository can read that source. That push
deploys like any other. Until the deployment succeeds, `volcano deploy`
refuses functions and frontends. Once it succeeds, this cannot be undone: the
repository is the project's source of truth, code deploys only by pushing to
it, and the project can never be disconnected from it or moved to another
repository. Variables, databases and other settings stay managed through
Volcano.

The command asks before it writes, naming the repository and branch. Pass
`--branch` to state the branch you expect, so a script fails instead of
exporting somewhere else, and `--yes` to skip the prompt. `--json` prints the
result:

```bash
volcano git export --branch main --yes --json
```

```json
{
  "branch": "main",
  "commit_sha": "6f2b1c9a4e8d3f7b0a5c2e9d1f4b8a6c3e7d0b25",
  "file_count": 12,
  "omitted": [{ "kind": "migrations", "path": "volcano/migrations", "resource": "" }],
  "repo_full_name": "acme/storefront",
  "skipped": []
}
```

`skipped` lists resources whose source could not be taken, each with its
reason; most often one that never deployed successfully. `omitted` lists what an export never writes: migrations,
variable values, credential-shaped files such as `.env`, and installed
dependencies.

Export runs once per project, and only into an empty repository. Volcano never
creates the repository. If your repository already holds the project, push to
it instead of exporting.

If GitHub does not confirm the push, run `volcano git export` again: it adopts
the same commit if it reached the repository. To stop an export that has not
finished and deploy from the CLI again:

```bash
volcano git export cancel
```

Canceling leaves the repository and any pushed commit alone. Once the first
commit was made, export cannot run again.

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
App settings, so `volcano git connect` and `volcano git repos` report that the
integration is not configured. Run these commands against the cloud API.

`volcano git disconnect` reads only the project's own binding, which does not
touch GitHub, so it reports that the project has nothing connected instead.
`volcano git root-directory`, `production-branch`, `auto-deploy`, `functions`
and `frontend` report that the integration is not configured, since no
repository can be connected there for them to change. `volcano git export`
reports that source export is not available: local mode serves functions from a
directory on your machine, so Volcano stores no source to export.
