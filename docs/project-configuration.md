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

The same manifest applies to local development and cloud projects.
`volcano config pull` downloads the current configuration as a canonical
manifest rendered by Volcano.

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
- Write-only secrets, such as SMTP passwords, OAuth client secrets, and TLS
  material, are omitted from `config pull` exports. Keep them in your
  environment and set them via `${ENV_VAR}` interpolation.

Behavior changes from older CLI releases:

- Buckets are no longer auto-created.
- An omitted `policies` key now leaves a bucket's policies untouched; older
  releases deleted them all.
- Schedulers are now deleted by omission within a declared `schedulers` list.
- The scheduler `regions` field is no longer supported. Placement is managed by
  Volcano.
