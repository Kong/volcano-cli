# Project configuration

`volcano config deploy` uploads a declarative manifest
(`volcano/volcano-config.yaml` or `./volcano-config.yaml`) to Volcano, which
validates and applies the full project configuration:

- Project settings
- Database requirements
- Variables
- Buckets and policies
- Realtime
- Auth configuration, including providers, email, templates, and managed pages
- Function visibility and schedulers
- Frontend custom domains

The same manifest applies to local development and cloud projects — only the
command namespace changes:

| Target | Export to file | Apply from file |
|---|---|---|
| Local dev (`volcano start`) | `volcano config pull` | `volcano config deploy` |
| Cloud project (`volcano login` + `volcano use`) | `volcano cloud config pull` | `volcano cloud config deploy` |

`pull` downloads the target's current configuration as a canonical manifest
rendered by Volcano; `deploy` uploads a manifest and reconciles the target to
match it. Both commands take the same flags (`-f/--file`, `--force` for
`pull`, `--dry-run` for `deploy`) regardless of namespace.

```yaml
version: 1
variables:
  - name: STRIPE_SECRET_KEY
    value: ${STRIPE_SECRET_KEY}  # interpolated from the CLI environment
realtime:
  enabled: true
functions:
  - name: hello
    public: false
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

## Coming from Terraform?

There is no state file (`.tfstate` or equivalent) behind `volcano-config.yaml`.
Every `deploy` — including `--dry-run` — asks Volcano to diff the manifest
against the project's actual live configuration, not a cached snapshot of a
prior apply. Practical implications:

- No `import` / `state rm` / `state mv`: point the manifest at an existing
  project and `deploy`; there's nothing to reconcile into a state file first.
- No drift-detection step: the plan is always computed against live
  configuration, so it can't disagree with reality the way a stale state file
  can.
- `--dry-run` is a live report, not a saved plan artifact — you can't inspect
  it later or hand it to a later `deploy`; running `deploy` always recomputes
  the plan from scratch.
- No workspaces, no `-target`: the whole manifest reconciles against the
  currently selected project (`volcano use` / `VOLCANO_PROJECT_ID`) as one
  unit. To leave something alone, omit it from the file.
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
