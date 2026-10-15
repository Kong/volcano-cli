---
title: "Connect a directory"
description: "Sign in, create a cloud project, and link a directory to it with one command, ready for a coding agent to build on."
---

## What it does

`volcano connect` goes from no login to a directory linked to a cloud project
in one command. Paste it into a coding agent, or run it yourself:

```bash
npx -y @volcano.dev/cli connect
```

It runs these steps, and is safe to run again:

1. **Signs in.** It reuses a login that still works, takes `--token`, or runs
   browser login. Browser login prints a code and a link. Open the link and
   approve, and the command continues.
2. **Creates a project.** It always creates a new project, named after the
   directory or `--name`. If you already have a project with that name, it
   adds `-2`, `-3`, and so on. With `--project <project-id>` it links that
   project instead. A rerun in a directory that is already linked keeps the
   linked project.
3. **Links the directory.** It writes `.volcano/project.json`. CLI commands
   run inside the directory use this project. See
   [Linked directories](#linked-directories).
4. **Creates the scaffold.** If `volcano/` is missing, it creates the same
   scaffold as `volcano init`. With `--template` it adds that starter.
5. **Writes the env file.** It writes the project's API URL and anon key to
   `volcano/cloud.env`, as `VOLCANO_API_URL`, `VOLCANO_ANON_KEY`, and the
   `NEXT_PUBLIC_` copies a Next.js frontend reads. Other lines in the file are
   kept.
6. **Updates `.gitignore` and `AGENTS.md`.** It gitignores
   `.volcano/project.json` and `volcano/*.env`, and adds a Volcano section to
   `AGENTS.md` that tells the agent where the project link is, which skills to
   use, and how to deploy. The section names no project, so teammates can
   share it. A rerun replaces it instead of adding another.
7. **Installs skills.** It installs the Volcano skills for every coding agent
   it finds on the machine, as `volcano setup --yes` does. See
   [setup.md](setup.md).

It ends with the project's name and its dashboard link:

```text
Project:   my-app
Dashboard: https://volcano.dev/dashboard/overview?project=<project-id>
```

## Usage

```bash
volcano connect                        # link this directory
volcano connect my-app                 # create ./my-app and link it
volcano connect --name shop            # name the new project
volcano connect --project <project-id> # link an existing project
volcano connect --template nextjs      # also add the Next.js starter
volcano connect --signup               # new to Volcano: create an account first
volcano connect --token pk-xxxxxxxxxx  # sign in with a token, no browser
volcano connect --json                 # print the result as JSON
```

| Flag | Meaning |
|---|---|
| `--project` | Link the existing project with this ID instead of creating one |
| `--name` | Name for the new project. Defaults to the directory name |
| `--template` | Starter to add: `nextjs`, `javascript`, `python`, `ruby` |
| `--signup` | Create an account in the browser first. Uses `--email`, else `git config --global user.email` |
| `--token` | Sign in with an account token instead of the browser |
| `--skip-skills` | Don't install skills |
| `--json` | Print the result as JSON on stdout. Progress goes to stderr |

## New accounts and the project limit

Run `volcano connect --signup` if you don't have an account yet. It opens
the signup page, and the CLI is signed in when you finish.

The free plan allows one project, and signup creates it. When the plan's
limit is reached, `connect` fails and lists your projects with the command to
link each one:

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
saves the project for every directory without a link.

The link is gitignored because each developer usually has their own project.
Remove the `.gitignore` entry to share it with your team.

## Running from an agent

`connect` never waits on a prompt when stdin is not a terminal. When it would
ask for a project name, it uses the directory name.

Browser login waits until you approve in the browser. An agent should show you
the code and link it prints, then wait for the command to finish. See
[scripting.md](scripting.md) for exit codes.
