---
title: "Connect a directory"
description: "Sign in, create a cloud project, and set up a directory for it with one command, ready for a coding agent to build on."
---

## What it does

`volcano connect` goes from no login to a new cloud project and a directory
linked to it, in one command. Paste it into a coding agent, or run it
yourself:

```bash
npx -y @volcano.dev/cli connect
```

It runs these steps. A rerun inside the linked directory, or with the same
directory argument, keeps the project and updates the files in place. A rerun
with no directory argument outside a linked directory creates another project.

1. **Signs in.** It reuses a login that still works, takes `--token`, or runs
   browser login. Browser login prints a code and a link. Open the link and
   approve, and the command continues. See
   [One-time tokens from the dashboard](#one-time-tokens-from-the-dashboard).
2. **Creates a project and its directory.** It always creates a new project.
   With no directory argument, it uses `--name` or a generated name such as
   `fuji-amber-42`, and creates a directory with the same name in the current
   directory. With a directory argument, the project is named after that
   directory. If the name is taken by one of your projects or an existing
   directory, it adds `-2`, `-3`, and so on. With `--project <project-id>` it
   uses that project instead. Run inside a directory that is already linked,
   it keeps the linked project.
3. **Links the directory.** It writes `.volcano/project.json`. CLI commands
   run inside the directory use this project. See
   [Linked directories](#linked-directories).
4. **Creates the scaffold.** If `volcano/` is missing, it creates the same
   scaffold as `volcano init`. With `--template` it adds that starter, also
   to a directory set up before. It keeps the scaffold files already there
   and adds the starter's missing lines to an existing `.gitignore`. When
   every starter file is already there, as on a rerun, it keeps the ones you
   changed and names them. Otherwise, if a starter file is already there with
   different content, `connect` fails before it creates a project or changes
   a file, and names the file.
5. **Writes the env file.** It writes the project's API URL and anon key to
   `volcano/cloud.env`, as `VOLCANO_API_URL`, `VOLCANO_ANON_KEY`, and the
   `NEXT_PUBLIC_` copies a Next.js frontend reads. The API URL is written
   without a user name, password, query, or fragment, because a frontend build
   makes it public. Other lines in the file are kept.
6. **Updates `.gitignore` and `AGENTS.md`.** It gitignores
   `.volcano/project.json` and `volcano/*.env`, and adds a Volcano section to
   `AGENTS.md` that tells the agent where the project link is, which skills to
   use, and how to deploy. The section names no project, so teammates can
   share it. A rerun replaces it instead of adding another.
7. **Installs skills.** It installs the Volcano skills for every coding agent
   it finds on the machine, as `volcano setup --yes` does. See
   [setup.md](setup.md).

It ends with the project's name, its directory, and its dashboard link:

```text
Project:   fuji-amber-42
Directory: ./fuji-amber-42
Dashboard: https://volcano.dev/dashboard/overview?project=<project-id>

Next: cd ./fuji-amber-42, read AGENTS.md there, and use the Volcano skills to build.
```

## Usage

```bash
volcano connect                        # new project in a new directory
volcano connect --name shop            # new project and directory named shop
volcano connect .                      # link this directory to a new project
volcano connect my-app                 # create ./my-app and link it
volcano connect --project <project-id> # set up an existing project
volcano connect --template nextjs      # also add the Next.js starter
volcano connect --signup               # new to Volcano: create an account first
volcano connect --token vbt_xxxxxxxx  # sign in with the dashboard's one-time token
volcano connect --token pk-xxxxxxxxxx  # sign in with an account token, no browser
volcano connect --json                 # print the result as JSON
```

| Flag | Meaning |
|---|---|
| `--project` | Use the existing project with this ID instead of creating one |
| `--name` | Name for the new project. Defaults to the directory argument's name, else a generated name |
| `--template` | Starter to add: `nextjs`, `javascript`, `python`, `ruby` |
| `--signup` | Create an account in the browser first. Uses `--email`, else `git config --global user.email`. Fails when `VOLCANO_TOKEN` is set |
| `--token` | Sign in with a one-time `vbt_` token from the dashboard, or an account token, instead of the browser. Fails when `VOLCANO_TOKEN` is set |
| `--skip-skills` | Don't install skills |
| `--json` | Print the result as JSON on stdout. Progress goes to stderr |

## One-time tokens from the dashboard

When you have no projects, the dashboard's Projects page shows a prompt to
paste into your coding agent. Each copy of the prompt holds a new one-time
token that starts with `vbt_`:

```bash
volcano connect --token vbt_xxxxxxxx
```

`connect` trades the token for a normal CLI login and saves it, so later
commands stay signed in. The token works once and expires 15 minutes after
you copy the prompt. If it has expired, was already used, or is refused, `connect`
warns and runs browser login instead. Copy the prompt again for a new token.

The login is saved as soon as the token is exchanged. If a later step fails,
the CLI is still signed in: run the command again without `--token`.

## New accounts and the project limit

Run `volcano connect --signup` if you don't have an account yet. It opens
the signup page, and the CLI is signed in when you finish.

When your plan's project limit is reached, `connect` fails and lists your
projects with the command to set up each one:

```bash
volcano connect --project <project-id>
```

## Linked directories

`.volcano/project.json` links a directory to a project. CLI commands run in
that directory or below it use the linked project. The CLI looks for the file
in the current directory and its parents, up to the repository root.

The project a command uses comes from, in order:

1. `VOLCANO_PROJECT_ID`
2. The directory's link
3. The project `volcano use` saved

`volcano use` run inside a linked directory updates the link. Elsewhere it
saves the project for every directory without a link. Inside a linked
directory, if `VOLCANO_PROJECT_ID` names a different project, `volcano use`
fails and changes nothing, because commands would keep using the project the
variable names. Unset it, or set it to the new project's ID, and run
`volcano use` again.

`volcano login --token <token> --project <project>` run inside a linked
directory updates the link the same way, with the same `VOLCANO_PROJECT_ID`
check. `volcano connect` checks it too, before it signs in or creates a
project: when `VOLCANO_PROJECT_ID` is set, it only links the project the
variable names, with `--project` or through an existing link. If the link named another project, it warns and prints the command that
links it back.

If the linked directory already has a `volcano/cloud.env` with an anon key,
`volcano use` and `volcano login --project` also rewrite the values `connect`
wrote there for the new project. If that fails, they warn, and
`volcano connect .` refreshes the file.

If the link can't be read, `volcano login` and `volcano signup` warn and
ignore it. `volcano docs` and local commands, such as
`volcano functions deploy`, ignore it because they never use a project. Other commands fail and name the file. `volcano connect` ignores
one in a parent of the directory it sets up. For one in that directory, it
stops unless `--project <project-id>` or `--name <name>` (for a new project)
says what replaces it.

The link is gitignored because each developer usually has their own project.
Remove the `.gitignore` entry to share it with your team.

## Running from an agent

`connect` never waits on a prompt when stdin is not a terminal, or when `CI`,
`VOLCANO_NONINTERACTIVE`, or `TERM=dumb` is set. When it would ask for a
project name, it uses the default name.

Without a directory argument, `connect` sets up a new directory below the
current one. The agent should `cd` into the `Directory:` it prints (`directory`
in `--json`) before running other `volcano` commands, and read the
`AGENTS.md` there.

Browser login waits until you approve in the browser. An agent should show you
the code and link it prints, then wait for the command to finish. See
[scripting.md](scripting.md) for exit codes.
