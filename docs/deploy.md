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

Before anything changes, the command reads and checks the env file, the
manifests, and `.volcano/source-layout.json`. A missing file named by a flag, a
broken manifest, or one of the refusals below stops the run with the project
untouched. Function sources and frontend directories are packaged later, in
their own steps. The run stops at the first step that fails, names the steps
that already ran, and lists the commands that finish the rest by hand. When
the first manifest isn't the default one, it only says to run it again:
`functions deploy` and `durable deploy` read the default manifest, so run by
hand they could deploy a durable function as a standard one.

New functions start `private`, and the config step then applies the
`visibility` the first manifest declares for them. The functions and durable
steps say so instead of telling you to run `config deploy`. A new function the
manifest gives no visibility stays `private`, and the run prints the commands
that open it.

The server only adds or changes a scheduler on an active function, and a
function the run just deployed is usually still building when the config step
runs. When a manifest adds or changes a scheduler on such a function, the config
step leaves out that function's schedulers and applies the rest of the manifest.
After the wait, the command applies just those schedulers, so values a later
manifest set stay in place. Schedulers that don't change apply in the config
step as usual. With `--no-wait`, or when the wait fails because any deployment
fails or times out, it prints the `config deploy` commands that apply the
schedulers later.

A deployment can be redeployed while the run waits, for example when the config
step changes a function's regions. The run then waits for the newer deployment,
since that is the one that serves. A durable function also has to accept
executions before the run counts it as deployed, so the run also waits while
its status is `provisioning`.

## Flags

| Flag | Description |
|---|---|
| `--with-migrations` | Apply every migration in `volcano/migrations`. |
| `-d, --database` | Database to migrate. Local mode defaults to `app`. In the cloud, the default is the database the manifests' `databases:` sections declare, if they declare exactly one. |
| `--env-file` | Env file to deploy. Default: `volcano/volcano.env` or `./volcano.env`. |
| `--no-env-file` | Deploy no env file, even when `volcano/volcano.env` exists. Use it when a manifest's `variables` section holds the project's variables. |
| `-c, --config` | Manifest to apply. Repeat it to apply several, in order. Passing any `-c` replaces default lookup: `./volcano-config.yaml` first, then `volcano/volcano-config.yaml` only when the root file is absent. |
| `--no-frontends` | Skip frontends. Useful while `npm run dev` is your edit loop, or when only function code changed. |
| `--no-wait` | Return without waiting for deployments to finish. |

`-d` without `--with-migrations` is refused, since it would do nothing.

The first manifest is the primary one. Function kinds, variable scopes, and
each frontend's variable selection come from it. A later manifest that declares
`kind: durable` or a frontend's `variable_scope` or `variables` is refused:
deployed from the wrong manifest, a durable function would be created as a
standard one, and a function's kind can't change. For the same reason, when
the project has a `volcano/functions` directory and `-c` leaves out the
default manifest while it declares durable functions the first manifest
doesn't, the run is refused.

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
files. Pass `--no-env-file` to deploy only the manifest's variables, for
example in a project whose `volcano/volcano.env` serves local runs.

For the same reason, only one manifest may declare each of `variables`,
`shared_variables`, and `frontend_shared_variables`. Two `-c` manifests that
declare the same section are refused.

Variables are deployed first, before any function or frontend that selects
them. The variables step deploys the env file, then applies just the
`variables`, `shared_variables`, and `frontend_shared_variables` sections of
each manifest that has them. The config step applies the full manifests later.

## Migrations

Migrations run only with `--with-migrations`, and without tracking: every file
runs again on each run. Write migrations that are safe to re-run, for example
with `create table if not exists`, and put one SQL statement in each file. When
a later step fails, run the command again without `--with-migrations` and
`-d`, since the migrations already ran.

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
`.gitignore` lists, `node_modules`, `.next`, `.env*.local` files, the Volcano
env files (`volcano/*.env` and `volcano.env`), and the env file the run
deployed, wherever it is.

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
with the selection it already has (none for a new frontend), and the config
step applies the entry afterwards. The run warns about each such frontend; run
the deploy again to rebuild it with the selection.

In the cloud, a declared frontend entry without `custom_domain` or
`custom_domains` leaves the frontend's domains alone. `custom_domain: null`
removes its domain, and `custom_domains` replaces the whole set. A server that
predates these rules refuses `custom_domain: null` in the dry run, and removes
the domain of an entry without `custom_domain`. Deploying a manifest written by
`volcano config pull` keeps every domain on either server, and the dry run
lists any domain it would delete.

## Source commit

A cloud frontend, function, or durable function deploy reports the commit its
source came from, so Volcano can tie the deployment to your repository. The CLI sends the GitHub repository as
`owner/name`, the commit ID, and whether the uploaded files differ from that
commit. A frontend deploy also sends the uploaded directory's path in the
repository, which is empty when it's the repository's top level. The CLI never
sends the remote URL or any credentials in it, local paths, branch names, or
diffs.

The CLI reads the commit checked out where the deploy reads its files, the
directory `--path` names for a frontend and the working directory for
functions: the `origin` remote and `HEAD`, the same in GitHub Actions as
anywhere else. It sends nothing, and the deploy goes ahead, when git isn't
available, the repository isn't on github.com or has no commit yet, the
uploaded files are outside the repository, or any part is missing. Local
deploys never send it.

### Frontend uploads

The upload matches the commit when each file in it is in the commit at the
same path with the same content and executable bit, and it holds every file
the commit has in the uploaded directory. Files every upload leaves out, such
as `node_modules`, `dist`, `build`, `.vscode`, `.gitignore`, and `*.log`, don't
count. Line endings count as the same where git converts them, as
`core.autocrlf` and the `text`, `eol`, and `crlf` attributes decide, and a file
whose `filter=lfs` attribute has Git LFS store it matches its pointer. The CLI
runs no other git filter, so a file another filter changes counts as changed.

Anything else counts as changed:

- An edited, moved, or deleted file.
- A file the commit doesn't have that the upload includes, such as an untracked
  file.
- A committed file the upload lacks, such as one replaced by a symlink or a
  directory, one in a directory a symlink replaced, one the root `.gitignore`
  lists, or the env file that `volcano deploy` deploys as variables instead of
  uploading. The upload never holds a symlink, so a committed symlink counts
  too.

The upload skips what the `.gitignore` at the root of the uploaded directory
lists, not what a `.gitignore` in a subdirectory or outside it lists, so a file
only those ignore is uploaded and counts as changed. Windows has no executable
bit, so a frontend deployed from Windows doesn't match a commit that holds an
executable file.

### Function uploads

Function and durable function deploys send the commit only for a clean
function, and nothing for one that has changed. A function is clean when
every committed file its upload is made from is in the upload as the commit
holds it, none of them is staged, and the upload holds no file the commit
doesn't have unless git ignores it. Those files are:

- The function's file, or the files in its directory that the upload keeps.
- The dependency manifests the upload picks up, such as a `package.json` next
  to the function or in a directory above it.
- Shared libraries: entries under `volcano/` whose names start with `_`.
- The `volcano-config.yaml` the deploy reads declarations from, and the root
  `.gitignore`, which decides what the upload leaves out.

A committed file the upload lacks counts as changed, such as one that was
deleted or replaced by a symlink or a directory, or one in a directory a
symlink replaced. So does a committed file that another file or a shared
library takes the place of, unless the commit or the upload holds that
replacement: an empty `volcano/functions/_shared/` left behind still replaces
a committed `volcano/_shared/`, and a checkout of the commit would upload the
committed one.

In a directory function's directory and in each shared library, a file git
neither tracks nor ignores counts as changed even when the upload leaves it
out, such as a `*.log` file or a `node_modules` directory that no
`.gitignore` lists. Changes anywhere else don't count, such as to another
function or to a file beside a single-file function that its upload doesn't
read. `functions deploy --all` checks each function on its own, so a batch
can send the commit for some functions and not for others.

A file git ignores is build output, such as a bundle your build writes to an
ignored `volcano/functions/`. The CLI can't tell which sources made it, so a
function whose upload holds one gets the commit only when the whole repository
matches `HEAD`: every tracked file has the content and, where git tracks it,
the executable bit that `HEAD` holds, nothing is staged, no file is untracked
unless git ignores it, and there's no submodule. Deploy from a clean checkout,
and commit any generated file the build changes.

Line endings and Git LFS pointers count as they do for frontends, and the CLI
runs no other git filter. A function with a submodule among its files, or one
whose files or `volcano-config.yaml` are reached through a symlink or are
outside the working directory, gets no commit.

### Verification

Volcano checks the commit against the project's connected repository and
records what it confirmed as `verification`. LavaFlow reads source at a commit
with either level below, never the uploaded files, so commit your changes
before you deploy:

- `files`, for frontend deploys: the commit is on the production branch, and
  the upload [matches it](#frontend-uploads). LavaFlow reads the commit for
  the deployment and for each redeploy of its build.
- `repository`, for function and durable function deploys: the commit is on
  the production branch. Volcano doesn't compare a function upload's files
  with the commit. LavaFlow reads the commit for that deployment only, not
  for a redeploy, such as one a variable change starts, and only while the
  project's repository link is unchanged since Volcano checked it. Relinking,
  even to the same repository, reinstalling the GitHub App, renaming the
  repository, or changing the production branch or root directory ends it. A
  fix opens only while the production branch still holds the commit, and a
  deployment at this level never counts as deploying a merged fix, so
  LavaFlow doesn't resolve the issue from it.

A reported commit Volcano can't confirm is recorded with `verification: none`,
and LavaFlow diagnoses that deployment from the logs alone. That includes a
frontend upload that doesn't match its commit, and a workflow run for a pull
request, which deploys GitHub's merge commit on no branch. At autonomy
`merge`, LavaFlow holds a fix built on a reported commit, at either level, for
a person to merge. A fix built on a commit that a
[push to the repository deployed](#cloud-projects-that-deploy-from-git) has no
such hold. `frontends deploy --from` keeps the commit of the build it
publishes. A frontend redeploy reports no commit of its own, and LavaFlow
reads it as the commit of the build that made its artifact.

To send nothing, set:

```bash
export VOLCANO_DEPLOY_COMMIT=off
```

## Cloud projects that deploy from Git

When a cloud project's code deploys from its connected repository, functions
and frontends can't be deployed directly. `volcano cloud deploy` refuses before
it changes anything. Push to the repository to deploy code, and use
`volcano cloud variables deploy -f volcano/cloud.env` and
`volcano cloud config deploy -f <manifest>`, once per manifest, for the rest.
The refusal prints those commands for the files the run read. Name the env
file: without `-f`,
`variables deploy` reads `volcano/volcano.env`, the local server's values.

## Related

- [Variables](variables.md)
- [Functions](functions.md)
- [Durable functions](durable-functions.md)
- [Frontends](frontends.md)
- [Declarative configuration](project-configuration.md)
