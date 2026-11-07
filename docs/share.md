---
title: "Share and fork"
description: "Share a cloud project with volcano share so anyone can fork their own copy with volcano fork, and see how often it is forked."
---

## What it is

`volcano share` shares your project: anyone with its link can fork their own
copy into their Volcano account. The link looks like
`https://volcano.dev/deploy/<id>`. It carries only the id, so it cannot be
edited to point at other code.

Sharing publishes a release: a snapshot of the functions and frontends the
project runs, which Volcano stores. People fork exactly that release. Changing
your project changes nothing for them until you share again.

`volcano fork <link>` is the other side: it creates your own cloud project from
a shared project's release.

## What a share carries

- Names only. A share never carries variable values, keys, sign-in settings,
  or secret files such as `.env`, `.envrc`, `credentials.json` or `.npmrc`.
  The files it leaves out are listed by name when you share.
- Every variable the project declares, which each copy gets empty for its owner
  to set.
- No database: a copy starts without one.
- Functions that read `VOLCANO_SERVICE_KEY` get each copy's own service key,
  which bypasses row-level security. `volcano share` asks you to confirm them.

A key written into the source code itself is not detected. Keep keys in
variables.

## Share a project

Run `volcano share` in your project directory, the one that holds `volcano/`
or is linked to the cloud project. In a subdirectory of a linked project it
refuses to deploy and stops, unless you pass `--no-deploy`. A project that
deploys from its Git repository is never deployed directly, so it is shared
from a subdirectory too.

```bash
volcano share --description "Ask a question, get a checked answer."
```

It shares what is in the directory:

1. A directory that is not on Volcano Cloud yet gets a cloud project, and the
   directory is linked to it, as `volcano connect .` links it.
2. The directory is deployed to its cloud project, as `volcano cloud deploy`
   deploys it, and the command waits for the deploy.
3. Volcano builds what the project runs and the command shows it: the project,
   its functions, frontends, variables, the service key functions and what is
   left out. Once you confirm, that build is published as the release, and the
   command prints the link, a Markdown deploy button for a README, and the same
   button in HTML.

The deploy sends no env file, because `volcano/volcano.env` holds the local
server's values. The cloud project's variables start empty. Pass
`--env-file <file>` to deploy your own values to your cloud project; the share
still carries only their names.

If a step fails, the command stops and says what exists. Run `volcano share`
again: it reuses the linked cloud project.

`--no-deploy` deploys nothing and creates no cloud project: it shares what the
linked or selected cloud project already runs.
A project that deploys from its Git repository is never deployed directly:
`volcano share` shares what it runs, and you push to the production branch to
share newer code. Outside a project directory, `volcano share` shares the
selected cloud project as it runs.

| Flag | What it does |
| --- | --- |
| `--description` | What the project does, on the link's page. One line, up to 280 characters. |
| `--allow-reshare` | Let people share their copies again. Off by default. |
| `--no-allow-reshare` | Stop copies from being shared again, including copies already shared. On an existing share it takes effect after any deploy and before the release is built, so it holds if you decline to publish. Add `--no-deploy` to change only that. |
| `--name` | The project name offered to people who fork it, for a new share. |
| `--env-file` | Env file to deploy to your cloud project with the directory. |
| `--no-deploy` | Share what the linked or selected cloud project runs now, without deploying or creating one. |
| `--link` | The share to use, by its link or id, when a project has several. Defaults to the newest share that is on. |
| `--yes` | Confirm without asking, including the service key functions. |
| `--json` | Print the share, the release, the link and the snippets as JSON. |

Run `volcano share` again whenever you want people to fork what you have then.

### When the project changes while you share

The release is the build you confirmed. If the project changes before it is
published, for example because a deploy finished in between, nothing is
published yet. At a terminal, `volcano share` shows the new build and asks you
again. With `--yes`, it publishes the new build once when it has no variable or
service key function that `--yes` did not already confirm, and otherwise shows
it and stops, so a function never gets the service key without your
confirmation.

### Limits

- A project can have up to 20 shares.
- Each share builds the project twice, once to show you and once to publish.
  An account can start 30 such builds an hour. When it reaches that, or
  Volcano is busy building, the command says when to try again.

## See and stop a share

```bash
volcano share status     # on or off, the link, the release people fork, opens and forks
volcano share releases   # every release shared, newest first
volcano share off        # stop sharing; copies already made keep working
```

Each takes `--link` and `--json`. `volcano share status` counts the last 30
days.

You can always switch sharing off, even when your account is read-only.
`volcano share` switches it on again, once the new release is published.

A share that is on does not always serve its link. `volcano share status` says
so when nothing is published yet, or when something else stops it: sharing is
not available in that environment, the project is being deleted, your account
does not allow traffic, or the project is a copy that may no longer be shared.

If Volcano takes a share down, for example after an abuse report,
`volcano share status` says so. You cannot switch it back on or delete it, and
the project cannot be shared again until Volcano lifts it. Contact support if
you think it is a mistake.

## Copies of copies

A copy cannot be shared again unless the project it came from allows it with
`--allow-reshare`. When the original owner takes that back, every copy down the
chain stops being shared: their links stop offering them, and `volcano share`
on a copy says re-sharing is not allowed. When the share a copy came from is
not available to share from right now, `volcano share` on the copy says so.

Re-sharing control is about consent, not secrecy: a copy's owner has its code
and can always put it in a new project of their own.

## Fork a shared project

```bash
volcano fork https://volcano.dev/deploy/k3Vn8TqPz0aX
volcano fork k3Vn8TqPz0aX my-copy
```

`volcano fork` shows what the release creates, the variables it declares, what
it leaves out, and the functions that get your copy's service key, and asks you
to confirm. It installs exactly that release, waits for it, and makes the copy
your active project. Inside a directory linked to another project, it leaves
that link alone and tells you how to select the copy.

No values are needed: the copy gets every variable empty, and the command lists
them. Set them in an env file and run
`volcano cloud variables deploy --file <env-file>`, or in the dashboard. That
command writes to your active project, so when the copy is not it, for example
inside a linked directory or after `volcano projects create --from`, first run
`volcano use <project-id>` outside a linked directory. `VOLCANO_PROJECT_ID`
outranks both, so unset it first. To fill one in up front, pass
`--var NAME=value`; repeat it for more.

If the owner shares a new release after you saw the preview, nothing is
created. At a terminal the command shows what changed and asks again; with
`--yes` it shows the new release and stops, so you never deploy a release you
have not seen.

An account can fork 20 times an hour. A fork that fails to install keeps the
project, and `volcano projects get <project-id>` says why it failed.

| Flag | What it does |
| --- | --- |
| `--var NAME=value` | Fill a variable in instead of leaving it empty. Repeatable. |
| `--yes` | Skip the confirmation. |
| `--no-wait` | Return once the copy is created. |
| `--json` | Print the created project as JSON. |

`volcano projects create <name> --from <link>` does the same as
`volcano fork`, without making the copy your active project.

## Where sharing is not available

Sharing is not on in every environment, and never in local mode. Where it is
off, `volcano share` and `volcano fork` say so before creating or deploying
anything, while `volcano share status`,
`volcano share releases` and `volcano share off` keep working for shares that
exist.
