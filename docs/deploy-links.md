---
title: "Deploy links"
description: "Create a deploy link for your project's public GitHub repository so anyone can deploy their own copy, and see how many people clicked and deployed."
---

## What it is

A deploy link lets anyone deploy the public GitHub repository your project is
connected to as their own Volcano project. Its address is
`https://volcano.dev/deploy/<id>`. The address carries only the id: it names no
repository or branch, so it cannot be edited to point at someone else's code,
and you can change what it follows without changing the link.

## How it relates

- Belongs to a **project** and deploys the repository from the project's
  [Git connection](git.md). Connect the project first, so `create` takes no
  repository argument.
- Deploy links are cloud-only. Local development has no Git connection, so
  `volcano deploy-link` there points you to `volcano cloud deploy-link`.
- A project can have up to 20 links.
- A person manages deploy links. Creating, enabling and deleting one needs an
  account token from `volcano login`, and a project access token is refused with
  `403`. A project access token can still list and show links and read their
  stats.
- Deploy links may not be available in every environment yet. Where they are
  not, `create` and `enable` fail with "Deploy links are not available in this
  environment." Listing, showing, `disable` and `delete` keep working.

## CLI operations

| Operation | Command |
|---|---|
| Create | `volcano cloud deploy-link create [--branch <branch>] [--path <dir>] [--name <name>]` |
| List | `volcano cloud deploy-link list` |
| Show | `volcano cloud deploy-link get <id>` |
| Stats | `volcano cloud deploy-link stats <id> [--days <n>]` |
| Switch off | `volcano cloud deploy-link disable <id>` |
| Switch on | `volcano cloud deploy-link enable <id>` |
| Delete | `volcano cloud deploy-link delete <id> [--yes]` |

Every command except `delete` accepts `--json` for machine-readable output. The
commands use the current project, which `volcano use` selects and
`VOLCANO_PROJECT_ID` overrides.

## Create a link

```bash
volcano cloud deploy-link create --branch main --path apps/web --name my-app
```

`--branch` is the branch the link follows and defaults to the project's
production branch. `--path` is a build root inside the repository, such as
`apps/web`, for a repository that holds several apps; it must be a clean
relative path. `--name` is the project name offered to visitors, and without it
Volcano chooses one.

The command prints the link id, its address, and snippets that show a deploy
button. Paste the Markdown into a README, or the HTML into a page:

```markdown
[![Deploy to Volcano](https://volcano.dev/deploy-button.svg)](https://volcano.dev/deploy/<id>)
```

```html
<a href="https://volcano.dev/deploy/<id>"><img src="https://volcano.dev/deploy-button.svg" alt="Deploy to Volcano"></a>
```

## What a link needs

Volcano checks all of this before it creates a link, and again when you enable
one. Nothing is created when a check fails.

| Requirement | Why |
|---|---|
| The project is connected to a GitHub repository | The link deploys that repository. |
| The repository is public | Only public repositories can be shared. |
| The connected GitHub account can push to it | That proves you control what the link deploys. |
| The branch exists | The link follows a branch. |
| The branch's latest commit has a frontend or a function | There must be something to deploy. |
| The repository does not ship `.volcano/template-bootstrap.sql` | Repositories with database SQL are not supported yet. |

## Switch a link off and on

```bash
volcano cloud deploy-link disable <id>
volcano cloud deploy-link enable <id>
```

Disabling needs no checks, so you can always do it. Enabling verifies the link
like a new one, so a link stays off while its repository is private or its
branch is gone. `get` shows why a link is off as its disabled reason:
`owner` when you turned it off, or `connection_lost`, `repository_private` or
`repository_changed` when it stopped being deployable.

## See clicks and deploys

```bash
volcano cloud deploy-link stats <id> --days 30
```

The output has the events and distinct visitors for each kind of event over the
window, then a daily table of the most recent days. Use `--json` for every day.
`--days` defaults to 30. Events are kept for 90 days, so a longer window counts
no more. The counts are for information only: visitors are counted from a salted
hash and Volcano stores no address.

## Delete a link

`delete` removes the link and its recorded events, and its address stops
working. Projects people already deployed from it are not affected. By default
it asks for confirmation; pass `--yes` to skip the prompt.

## Deploy from a link

Anyone with an account can create a project from a link:

```bash
volcano projects create my-app --from https://volcano.dev/deploy/<id>
```

`--from` takes the link's address or its 12-character id. Only the id is used:
the CLI sends it to the API it is configured for, whatever host the address
names.

The command first shows what the link deploys, which needs no sign-in on the
server's side: the repository and its address, the branch, the exact commit, and
the frontends and functions that would be created. Volcano has not reviewed this
code, so deploy only repositories you trust. Then it asks you to confirm that
commit. The project is pinned to the commit you confirmed, and the command waits
for the installation, printing each phase, until the project is ready.

| Flag | Does |
|---|---|
| `--yes` | Skips the confirmation, for CI. |
| `--commit <hash>` | Stops unless the link is at that commit (at least 7 characters), so a script deploys only code it expects. |
| `--no-wait` | Returns as soon as the project is created. Check it with `volcano projects get <project-id>`. |

`--from` cannot be combined with `--template`. `--commit`, `--yes` and
`--no-wait` only apply with `--from`.

If the link's branch moves after the preview, nothing is created. At a terminal
the command shows the new commit and asks again. With `--yes` or `--commit` it
shows the new commit, exits with an error and creates nothing, so a commit you
have not seen is never deployed: review it and run the command again.

If the installation fails, the command exits with an error and the project is
kept. Inspect it with `volcano projects get <project-id>`, or delete it with
`volcano projects delete <project-id>`. Once it is ready, `volcano use
<project-id>` selects it.

The Free plan allows one project, so creating another is refused until you delete
one or upgrade. A link that does not exist, was switched off, or can no longer be
deployed all answer "This deploy link is not available."

## When a command is refused

The CLI prints the API's message with what to do next.

| Status | Meaning |
|---|---|
| `401` | The GitHub connection's authorization expired, or you are signed out. Reconnect GitHub, or run `volcano login`, and try again. |
| `403` | The project is not yours, or a project access token was used. A person manages deploy links, so sign in with `volcano login`. |
| `404` | The project or link does not exist. For `--from`, "This deploy link is not available." With "Deploy links are not available in this environment." it is not a missing link: deploy links are not offered here. |
| `409` | The project has no repository connected, is connected to a different repository than the link, has 20 links already, or the link changed while it was being saved. For a link that changed, run the command again. For `--from`, `commit_changed` means the branch moved after the preview; see above. |
| `422` | The repository is private or empty, its configuration is invalid, the connected account cannot push to it, the branch does not exist, or the repository cannot be deployed as it is. |
| `503` | GitHub is rate limiting or not available right now, or the GitHub integration is not configured. Try again shortly. |
