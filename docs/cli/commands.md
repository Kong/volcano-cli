# CLI command reference

A complete reference for the `volcano` command tree. For authoritative,
always-current flags on any command, run `volcano <command> --help`.

## Usage

```bash
volcano [command] [subcommand] [flags]
volcano --help          # top-level commands
volcano <command> --help
```

Global flags:

- `-v, --version` — print the CLI version.
- `-h, --help` — help for any command.

## Local vs. cloud target

Most resource commands exist both as a **top-level** command and under the
**`cloud`** group:

- Top-level (e.g. `volcano functions list`) acts on your **active context** —
  the local development environment when it is running, otherwise the cloud
  project.
- The `cloud` prefix (e.g. `volcano cloud functions list`) always targets the
  **cloud** project.

## Getting started

- **`signup`** — Create a Volcano account.
- **`login`** — Authenticate with Volcano (browser or `--token` for CI).
- **`logout`** — Log out of Volcano.
- **`init`** — Create a local Volcano project scaffold. Optional template:
  `javascript` (aliases `js`, `node`, `nodejs`), `nextjs`, `python`, `ruby`.
- **`use`** — Set the active project.
- **`upgrade`** — Upgrade the Volcano CLI to the latest release.
- **`version`** — Print version information.

## Local development environment

- **`start`** — Start the local Volcano development environment.
- **`stop`** — Stop the local Volcano development environment.
- **`restart`** — Restart the local Volcano development environment.
- **`status`** — Show the status of the local Volcano environment.
- **`reset`** — Reset local development data in-place.

## Projects

- **`projects create`** — Create a project.
- **`projects delete`** — Delete a project.
- **`projects get`** — Get project details.
- **`projects list`** — List projects.
- **`projects use`** — Set the active project.

## Functions

Top-level `functions …` (active context) or `cloud functions …` (cloud).

- **`functions deploy`** — Deploy functions (`--all` for every function).
- **`functions list`** — List functions.
- **`functions get`** — Get a function.
- **`functions update`** — Update function settings.
- **`functions delete`** — Delete a function.
- **`functions invoke`** — Invoke a function.
- **`functions logs`** — Show function build or runtime logs.
- **`functions runtimes`** — List supported function runtimes.
- **`functions alias set|list|delete`** — Manage function invoke aliases.
- **`functions schedulers create|list|enable|disable|delete`** — Manage
  scheduled function invocations.

## Databases

Top-level `databases …` (active context) or `cloud databases …` (cloud).

- **`databases create`** — Create a database.
- **`databases list`** — List databases.
- **`databases get`** — Get a database.
- **`databases delete`** — Delete a database.
- **`databases migration up`** — Apply migrations for a database.

## Migrations

- **`migrations deploy`** — Apply migrations (`--all -d <db>`).

## Storage

Top-level `storage …` (active context) or `cloud storage …` (cloud).

- **`storage bucket create|list|get|update|delete`** — Manage storage buckets.
- **`storage object list|upload|download|copy|move|delete|visibility`** —
  Manage objects within a bucket (`download -` writes to stdout).
- **`storage policy create|list|get|delete`** — Manage bucket policies.
- **`storage stats`** — Show aggregate storage usage for the current project.

## Frontends

Available under the cloud group: `cloud frontends …`.

- **`cloud frontends deploy`** — Deploy a frontend.
- **`cloud frontends redeploy`** — Redeploy a frontend.
- **`cloud frontends list`** — List frontends.
- **`cloud frontends get`** — Get a frontend.
- **`cloud frontends delete`** — Delete a frontend.
- **`cloud frontends logs`** — Show frontend build or runtime logs.
- **`cloud frontends domain create|list|get|delete`** — Manage BYOC custom
  domains for a frontend.

## Variables

Top-level `variables …` (active context) or `cloud variables …` (cloud).

- **`variables deploy`** — Deploy variables from `volcano.env`.
- **`variables list`** — List variables.
- **`variables get`** — Get a variable.
- **`variables delete`** — Delete a variable.

## Declarative configuration

Top-level `config …` (active context) or `cloud config …` (cloud).

- **`config deploy`** — Deploy project configuration from YAML.
- **`config pull`** — Download the current project configuration as YAML.

## Documentation (`docs`)

Fetch, search, and read the Volcano documentation locally. See
[docs-search.md](../docs-search.md) for the full guide.

- **`docs sync`** — Fetch or refresh the local docs cache.
- **`docs search <query>`** — Search the docs; ranked results with snippets.
- **`docs get <path[#anchor]>`** — Read a document or a single section.
- **`docs list`** — List available documents.
- **`docs mcp`** — Run a Model Context Protocol server (stdio) exposing the
  docs as agent tools with a resident index.
