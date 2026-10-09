---
title: "Project configuration"
description: "Manage a project declaratively with volcano config: pull and deploy a volcano-config.yaml manifest that Volcano validates and applies."
---

`volcano config deploy` uploads a declarative manifest
(`volcano/volcano-config.yaml` or `./volcano-config.yaml`) to Volcano, which
validates and applies the full project configuration:

- Project settings
- Database requirements
- Variables and function or frontend shared variable names
- Buckets and policies
- Realtime
- Auth configuration, including providers, email, templates, and managed pages
- Function visibility, invocation mode, HTTP authentication, OpenAPI metadata,
  and schedulers
- Frontend custom domains and function routes
- Sandbox template image assertions and default session timeouts

The same manifest applies to local development and cloud projects — only the
command namespace changes:

| Target | Export to file | Apply from file |
|---|---|---|
| Local dev (`volcano start`) | `volcano config pull` | `volcano config deploy` |
| Cloud project (`volcano login` + `volcano use`) | `volcano cloud config pull` | `volcano cloud config deploy` |

`config pull` downloads the target's current configuration as a canonical
manifest rendered by Volcano; `config deploy` uploads a manifest and
reconciles the target to match it. Both take the same flags (`-f/--file`,
`--force` for `pull`, `--dry-run` for `deploy`) regardless of namespace.

```yaml
version: 1
variables:
  - name: STRIPE_SECRET_KEY
    value: ${STRIPE_SECRET_KEY}  # interpolated from the CLI environment
realtime:
  enabled: true
functions:
  - name: hello
    visibility: public         # private, authenticated, or public; omit to keep the current level
    variable_scope: scoped     # only the variables this function needs
    variables:
      - STRIPE_SECRET_KEY
    invocation_mode: http
    http_auth_mode: none
    openapi_spec:
      openapi: 3.1.0
      info: { title: Hello API, version: 1.0.0 }
      paths: {}
    schedulers:
      - name: refresh-cache      # required, unique per function (the reconcile key)
        cron: "*/5 * * * *"
        enabled: true
        payload: { job: refresh }
  - name: order-pipeline
    kind: durable              # standard or durable; asserted, not applied, since a kind is fixed at creation
frontends:
  - name: web
    function_routes:           # the complete set; routes left out are deleted
      - function: hello
        path_prefix: /api/hello
        strip_prefix: true
```

Cloud example — export the current cloud project's configuration to a file,
edit it, and apply it back:

```sh
volcano login
volcano use my-project
volcano cloud config pull -f volcano-config.yaml   # export to file
$EDITOR volcano-config.yaml
volcano cloud config deploy -f volcano-config.yaml --dry-run   # preview
volcano cloud config deploy -f volcano-config.yaml             # apply
```

Key semantics:

- `functions[].kind` declares which entries are
  [durable functions](durable-functions.md). It is asserted rather than applied,
  since a function's kind is fixed when it is created. It is also what tells the
  deploy commands apart: `volcano functions deploy --all` skips these entries and
  `volcano cloud durable deploy --all` deploys exactly them. Leave it out for a
  standard function, and leave the invocation settings out of a durable one —
  they describe synchronous HTTP invocation, which a durable function does not
  have.
- Declared config sections are the source of truth. Variables, bucket policies,
  OAuth providers, email templates, function schedulers, and frontend function
  routes are fully synced when declared: entries absent from the manifest are
  deleted. Omitted sections and fields keep their existing values.
- Functions, frontends, databases, and buckets are never created or deleted
  through the manifest; only their configuration is updated. A manifest entry
  for a resource that does not exist is skipped with a warning. A deployed
  resource missing from a declared section is reported too. For a new
  frontend, run `volcano cloud frontends deploy` with the `--variable-scope`
  and `--variable` flags its entry needs, then `volcano cloud config deploy`.
  A frontend created without them starts with no project variables.
- `${ENV_VAR}` references are interpolated before upload. A reference to an
  unset variable is an error, and `$$` produces a literal `$`.
- `volcano config deploy --dry-run` prints the projected actions without
  changing anything. Validation failures exit non-zero with Volcano's error
  list, and nothing is applied.
- If some entries fail to apply (a provider call failing mid-deploy),
  `config deploy` still prints a full report — succeeded entries included —
  and then exits non-zero because `summary.errors > 0`. Already-applied
  changes are not rolled back. Re-running `config deploy` with the same file
  is always safe: entries that already landed report `unchanged`, and only
  the entries that failed or still differ are retried.
- Variable values and write-only secrets, such as SMTP passwords, OAuth client
  secrets, and TLS material, are omitted from `config pull` exports. Keep them in your
  environment and set them via `${ENV_VAR}` interpolation. If a server does
  return variable values, `config pull` removes the whole `variables` section
  before writing the file, so no values reach disk and the export stays
  deployable.
- `functions[].variables` is fully synced when declared: the list replaces the
  function's declared variable names. Omitting it, like omitting
  `variable_scope`, leaves the function's existing declaration untouched. See
  "Function variable scope" below.

## Function visibility

`functions[].visibility` decides who can invoke a function:

- `private`: service keys and schedulers only.
- `authenticated`: also your project's signed-in users, including
  [anonymous sign-ins](/platform/authentication/anonymous-users).
- `public`: also anon keys with `functions.invoke`, and frontend function
  routes.

New functions start `private`, durable ones included. Leaving `visibility` out
keeps the level the function already has. Any other value is refused before
upload, naming the function.

A `private` function answers every caller but a service key or scheduler with
the `404` of a function that does not exist. An anon key invoking an
`authenticated` function by ID gets `403`; by name, as the SDK invokes, `404`.
If your app gets 404 for a function you deployed, check its visibility with
`volcano cloud functions get <name>`.

The deprecated `public` field still works, but `public: false` means
`authenticated`, not `private`: it lets your signed-in users in. Manifests
written by an older `config pull` contain it. To keep such a function
private, replace it with `visibility: private`. `true` means `public`.
`config deploy` and the function deploys print a warning for each `public`
they read. When a function declares both, `visibility` wins, and the server
rejects the pair only when exactly one of them says public, such as
`visibility: authenticated` with `public: true`. `config pull` writes
`visibility` only.

```yaml
version: 1
functions:
  - name: notes-summary
    visibility: authenticated   # called by signed-in users from the dashboard
  - name: nightly-report
    visibility: private         # only schedulers and server-side code
```

## Frontend function routes

`frontends[].function_routes` forwards every request under a path of a
frontend to a function, so the browser calls it on the frontend's own origin:

```yaml
version: 1
functions:
  - name: session
    visibility: public
    invocation_mode: http
frontends:
  - name: web
    function_routes:
      - function: session
        path_prefix: /api/session
        strip_prefix: true
```

| Field | Required | Meaning |
|---|---|---|
| `function` | Yes | A deployed standard function that is `public` with `invocation_mode: http`. |
| `path_prefix` | Yes | The path to forward, such as `/api/session`. It matches that path and everything under it. It starts with `/` and does not end with one. |
| `strip_prefix` | No | `true` sends the function the rest of the path, or `/` for the prefix itself. The default, `false`, sends the full path. |

The list is the frontend's complete set of routes:

- Omitting `function_routes` keeps the frontend's routes as they are.
- Declaring it replaces them: a route left out of the list is deleted.
- `function_routes: []` deletes every route on the frontend.

A route reaches the function without a Volcano credential, so anyone who can
load the frontend can call the function under its prefix. The function must
authenticate its callers itself. A frontend can have up to 64 routes, and the
longest matching prefix wins. `config pull` writes the routes back, so a pulled
manifest deploys again unchanged.

One deploy can make a function public and add its route, or delete a route and
make the function private. A route to a function that stays non-public fails
the dry run and nothing is applied. See [frontends](frontends.md#function-routes).

## Shared variable names

Use `shared_variables` to select the complete list of existing project variables
shared with functions, without changing their values:

```yaml
version: 1
shared_variables:
  - LOG_LEVEL
  - SERVICE_URL
```

Names are case-sensitive, must be unique, and must already exist. Each name must
start with a letter or underscore and contain only letters, digits, or
underscores. Supply names only, not objects containing values.

Omitting `shared_variables` leaves membership unchanged. Declaring
`shared_variables: []` clears the shared list. Names left out of a declared list
remain stored as non-shared variables; this does not delete their values.
The separate `variables` section still has its own full-sync semantics.

`config pull` exports shared names only and omits variable values. The exported
list can be deployed back to the same project without supplying those values.
Use `config deploy --dry-run` to preview a membership change.

## Frontend variable scope

Use `frontend_shared_variables` to select the complete list of existing project
variables shared with frontends:

```yaml
version: 1
frontend_shared_variables:
  - NEXT_PUBLIC_VOLCANO_API_URL
  - NEXT_PUBLIC_VOLCANO_ANON_KEY
frontends:
  - name: web
    variable_scope: shared
```

Declaring `frontend_shared_variables` replaces the complete frontend shared
list. Omitting it preserves the current membership, and declaring
`frontend_shared_variables: []` clears the list. Each name must already exist
as a project variable.

For a frontend, `variable_scope` accepts:

- `all` to use all project variables.
- `shared` to use `frontend_shared_variables`.
- `scoped` to use the names in that frontend's `variables` list.

Hosting rejects a frontend environment over 4,096 bytes before changing state.
`NEXT_PUBLIC_*` variables are build-only. Rebuild each frontend when changed
values must be embedded in browser assets.

Run `volcano config pull` before editing the manifest. Preserve each frontend's
`custom_domain` in the file when you deploy it back.

## Function variable scope

By default a function receives every project variable. Set `variable_scope` to
`scoped` to give it only the variables it actually needs:

```yaml
version: 1
variables:
  - name: STRIPE_SECRET_KEY
    value: ${STRIPE_SECRET_KEY}
  - name: SENDGRID_API_KEY
    value: ${SENDGRID_API_KEY}
functions:
  - name: charge
    variable_scope: scoped
    variables:
      - STRIPE_SECRET_KEY
  - name: notify
    variable_scope: scoped     # SENDGRID_API_KEY is read directly, so detection finds it
```

Because `functions deploy` reads these declarations from the manifest, every
`${VAR}` reference in the file must be set in the deploying shell. If one is
not, the deploy stops before uploading rather than continuing without the scope
declared here. See [functions](functions.md).

`variable_scope` takes `all` or `scoped`:

- `all` is the default and gives the function every project variable.
- `scoped` gives it the names listed in `variables`, plus the names Volcano
  detects in the function's source that the project defines.

Volcano reads the uploaded source and adds direct environment references it
finds there, so a variable the function reads by its literal name does not need
to be listed. List a name in `variables` when:

- the function reads it through a computed key, which detection cannot see, or
- the function must not deploy without it.

That difference matters on apply. A name you declare that the project does not
define fails the deploy; a name Volcano merely detected that the project does
not define is ignored, since such a reference is often optional.

A scoped function whose resulting environment exceeds 4096 bytes is rejected
before anything is deployed.

Both fields are optional and independent of each other. Omitting them sends
nothing, so an existing function keeps whatever scope it already has, and a
manifest written before scoping existed behaves exactly as it did.

## Coming from Terraform?

There is no state file (`.tfstate` or equivalent) behind `volcano-config.yaml`.
Every `config deploy` — including `--dry-run` — asks Volcano to diff the
manifest against the project's actual live configuration, not a cached
snapshot of a prior apply. Practical implications:

- No `import` / `state rm` / `state mv`: point the manifest at an existing
  project and run `config deploy`; there's nothing to reconcile into a state
  file first.
- No separate drift-detection step: there's no stored copy to go stale —
  every plan reads live configuration directly, so any actual drift just
  shows up as the next diff and gets reconciled.
- `--dry-run` is a live report, not a saved plan artifact — you can't inspect
  it later or hand it to a later `config deploy`; running `config deploy`
  always recomputes the plan from scratch.
- No workspaces, no `-target`: the whole manifest reconciles against the
  currently selected project (`volcano use` / `VOLCANO_PROJECT_ID`) as one
  unit. Omitting an entire section or field leaves it untouched. That does
  **not** apply within a section you've already declared as fully synced
  (`variables`, `buckets[].policies`, `auth.providers.oauth`,
  `auth.email.templates`, `functions[].schedulers`,
  `functions[].variables`, `frontends[].function_routes`) — omitting one entry
  from an otherwise-declared list still deletes that entry, since the
  declared list is the source of truth for the whole section. See "Key
  semantics" above.
- A failed deploy doesn't need an explicit resume step — see the apply-phase
  failure bullet above; re-running the same manifest picks up only what's
  still outstanding.

See the server-side
[configuration manifest reference](/platform/projects/configuration)
for the full reconciliation semantics.

Behavior changes from older CLI releases:

- Buckets are no longer auto-created.
- An omitted `policies` key now leaves a bucket's policies untouched; older
  releases deleted them all.
- Schedulers are now deleted by omission within a declared `schedulers` list.
- The scheduler `regions` field is no longer supported. Placement is managed by
  Volcano.

## Frontend variable scope

An existing frontend can select exactly which project variables its build and
runtime receive:

```yaml
version: 1
frontends:
  - name: web
    variable_scope: scoped
    variables:
      - NEXT_PUBLIC_API_URL
      - SESSION_SECRET
```

Apply with `volcano config deploy` (local) or `volcano cloud config deploy`.
The server must support frontend variable scopes. Missing declared variables
reject apply. Omitting a field preserves it; `variables: []` clears the selection.
`all` keeps the legacy frontend behavior of reading all project variables, not
just the function shared list. `NEXT_PUBLIC_*` variables are used during build
and excluded from runtime. Keep any existing custom-domain declaration in the
entry. Rebuild the frontend to change values embedded in browser assets.

## Sandbox template settings

Declare an existing template by name. `memory_mb` and `ports` assert the deployed
image settings; changing them requires a new source deployment. Configuration
apply updates `ttl_seconds`, the default VM lifetime for new sessions.

```yaml
version: 1
sandboxes:
  - name: local-custom
    memory_mb: 1024
    ports: [8080]
    ttl_seconds: 600
```

Preview with `volcano config deploy --dry-run`, apply with `volcano config deploy`,
and export with `volcano config pull --force`. Add `cloud` after `volcano` for a
cloud project. Omitted fields preserve current settings. Cloud TTL must be
30–28800 seconds. Local mode accepts `0` for unlimited lifetime or a positive TTL.
`idle_timeout_seconds` is no longer supported; remove it from existing manifests.
Config apply does not build images or delete templates
omitted from the manifest. See [Sandbox source deployment](sandboxes.md).
