---
title: "Deploy a project"
description: "Deploy variables, migrations, functions, durable functions, frontends, and configuration with one command, in the order Volcano needs."
---

`volcano deploy` deploys everything the project directory describes to the
local development server. `volcano cloud deploy` does the same for the selected
cloud project.

```bash
volcano start
volcano deploy --with-migrations
```

```text
Deploying to local project "my-app"
  1. variables   volcano/volcano.env
  2. migrations  volcano/migrations -> database app
  3. functions   volcano/functions
  4. durable     skipped: volcano/volcano-config.yaml declares no durable functions
  5. frontends   web (.volcano/source-layout.json)
  6. config      volcano/volcano-config.yaml

[1/6] variables
...
Waiting for 2 deployment(s)...
  ✓ notes-summary  active
  ✓ web            active

Frontends:
  web  http://3f9c1d2e-....frontends.localhost:8080

✓ Deployed in 1m42s
```

## What it deploys

The steps run in this order. A step with nothing to deploy is skipped.

| Step | Reads | Same as |
|---|---|---|
| variables | the env file, or the variable sections of the manifests | `variables deploy` |
| migrations | `volcano/migrations/*.sql`, only with `--with-migrations` | `migrations deploy --all` (cloud: `databases migration up --all`) |
| functions | the standard functions in `volcano/functions` | `functions deploy --all` |
| durable | the functions the first manifest declares `kind: durable` | `durable deploy --all` |
| frontends | the frontends listed in `.volcano/source-layout.json` | `frontends deploy` |
| config | each manifest, in the order given | `config deploy` |

Then the command waits until every deployment it started has finished, prints
each result, and prints the frontend URLs. It exits non-zero when a deployment
fails, and prints the command that shows that deployment's build log. Pass
`--no-wait` to return as soon as the config step is done.

Before anything changes, the command reads and checks every input file. A
missing file named by a flag, a broken manifest, or one of the refusals below
stops the run with the project untouched. After that, the run stops at the
first step that fails, names the steps that already ran, and lists the commands
that finish the rest by hand.

## Flags

| Flag | Description |
|---|---|
| `--with-migrations` | Apply every migration in `volcano/migrations`. |
| `-d, --database` | Database to migrate. Local mode defaults to `app`. In the cloud, the default is the database the manifests' `databases:` sections declare, if they declare exactly one. |
| `--env-file` | Env file to deploy. Default: `volcano/volcano.env` or `./volcano.env`. |
| `-c, --config` | Manifest to apply. Repeat it to apply several, in order. Passing any `-c` replaces the default `volcano/volcano-config.yaml` or `./volcano-config.yaml`. |
| `--no-frontends` | Skip frontends. Useful while `npm run dev` is your edit loop, or when only function code changed. |
| `--no-wait` | Return without waiting for deployments to finish. |

`-d` without `--with-migrations` is refused, since it would do nothing.

The first manifest is the primary one. Function kinds, variable scopes, and
each frontend's variable selection come from it. A later manifest that declares
`kind: durable` or a frontend's `variable_scope` or `variables` is refused:
deployed from the wrong manifest, a durable function would be created as a
standard one, and a function's kind can't change.

```bash
volcano deploy --with-migrations --env-file volcano/volcano.local.env \
  -c volcano/volcano-config.yaml -c volcano/auth.local.yaml
```

## Variables

`volcano/volcano.env` holds the values for the local development server. Keep
the cloud project's values in another file and name it on every cloud deploy:

```bash
volcano cloud deploy --env-file volcano/cloud.env
```

Projects that `volcano init` creates use the same names in both files. A
Next.js frontend reads its public copies, such as `NEXT_PUBLIC_VOLCANO_API_URL`
and `NEXT_PUBLIC_VOLCANO_ANON_KEY`, because builds never see `VOLCANO_*` names.
`volcano cloud deploy` refuses an env file that holds the local server's keys
or a `VOLCANO_API_URL` on `localhost`, which a cloud frontend would compile in.

Variables come from the env file or from a manifest's `variables` section,
never both. Applying a `variables` section deletes every variable it does not
list, so it would delete the ones only the env file sets. When an env file is
found and a manifest declares `variables`, the command refuses and names both
files.

Variables are deployed first, before any function or frontend that selects
them. The variables step deploys the env file, then applies just the
`variables`, `shared_variables`, and `frontend_shared_variables` sections of
each manifest that has them. The config step applies the full manifests later.

## Migrations

Migrations run only with `--with-migrations`, and without tracking: every file
runs again on each run. Write migrations that are safe to re-run, for example
with `create table if not exists`, and put one SQL statement in each file. When
a later step fails, run the command again without `--with-migrations`, since
the migrations already ran.

## Frontends

Frontends are listed in `.volcano/source-layout.json` at the project root. A
connected Git repository reads the same file, so the CLI and Git deploy the
same frontends.

```json
{
  "version": 1,
  "frontends": [
    { "name": "web", "workspace": "", "app_root": "web" }
  ]
}
```

| Field | Description |
|---|---|
| `name` | Frontend name: lowercase letters, digits, and inner hyphens. |
| `workspace` | Directory to upload, relative to the project root. `""` is the root. Omitted means `frontends/<name>`. |
| `app_root` | The Next.js app inside `workspace`. `""` is the workspace itself. |

`app_root` must contain the app's `package.json`, or be the directory that a
parent `package.json` builds with `next build <dir>`, as in projects that
`volcano init nextjs` creates. The upload skips what the workspace's root
`.gitignore` lists, `node_modules`, `.next`, `.env*.local` files, and the
Volcano env files (`volcano/*.env` and `volcano.env`).

Each frontend is built with the variable selection its entry under
`frontends:` in the primary manifest declares:

```yaml
frontends:
  - name: web
    variable_scope: scoped
    variables: [NEXT_PUBLIC_API_URL]
```

`NEXT_PUBLIC_*` values are compiled into the build, so the selection has to be
in place when the frontend builds, not only when config applies. A frontend
deploy can set `all` or `scoped` with its `variables`. A frontend with
`variable_scope: shared`, or an entry that leaves out either field, builds
with the selection it already has, and the config step applies the entry
afterwards.

In the cloud, a declared frontend entry without `custom_domain` removes the
frontend's custom domain. Keep `custom_domain` in the entry if the frontend has
one.

## Cloud projects that deploy from Git

When a cloud project's code deploys from its connected repository, functions
and frontends can't be deployed directly. `volcano cloud deploy` refuses before
it changes anything. Push to the repository to deploy code, and use
`volcano cloud variables deploy` and `volcano cloud config deploy` for the rest.

## Related

- [Variables](variables.md)
- [Functions](functions.md)
- [Durable functions](durable-functions.md)
- [Frontends](frontends.md)
- [Declarative configuration](project-configuration.md)
