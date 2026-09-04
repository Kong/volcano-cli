---
title: "Project configuration"
description: "Manage a project declaratively with volcano config: pull and deploy a volcano-config.yaml manifest that Volcano validates and applies."
---

`volcano config deploy` uploads a declarative manifest
(`volcano/volcano-config.yaml` or `./volcano-config.yaml`) to Volcano, which
validates and applies the full project configuration:

- Project settings
- Database requirements
- Variables
- Buckets and policies
- Realtime
- Auth configuration, including providers, email, templates, and managed pages
- Function visibility, invocation mode, HTTP authentication, OpenAPI metadata,
  and schedulers
- Frontend custom domains

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
    public: true
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

- Declared config sections are the source of truth. Variables, bucket policies,
  OAuth providers, email templates, and function schedulers are fully synced
  when declared: entries absent from the manifest are deleted. Omitted sections
  and fields keep their existing values.
- Functions, frontends, databases, and buckets are never created or deleted
  through the manifest; only their configuration is updated. A manifest entry
  for a resource that does not exist is skipped with a warning. A deployed
  resource missing from a declared section is reported too.
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
- Write-only secrets, such as SMTP passwords, OAuth client secrets, and TLS
  material, are omitted from `config pull` exports. Keep them in your
  environment and set them via `${ENV_VAR}` interpolation.
- `functions[].variables` is fully synced when declared: the list replaces the
  function's declared variable names. Omitting it, like omitting
  `variable_scope`, leaves the function's existing declaration untouched. See
  "Function variable scope" below.

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
  `functions[].variables`) — omitting one entry
  from an otherwise-declared list still deletes that entry, since the
  declared list is the source of truth for the whole section. See "Key
  semantics" above.
- A failed deploy doesn't need an explicit resume step — see the apply-phase
  failure bullet above; re-running the same manifest picks up only what's
  still outstanding.

See the server-side
[configuration manifest reference](https://github.com/Kong/volcano-hosting/blob/main/docs/projects/configuration.md)
for the full reconciliation semantics.

Behavior changes from older CLI releases:

- Buckets are no longer auto-created.
- An omitted `policies` key now leaves a bucket's policies untouched; older
  releases deleted them all.
- Schedulers are now deleted by omission within a declared `schedulers` list.
- The scheduler `regions` field is no longer supported. Placement is managed by
  Volcano.
