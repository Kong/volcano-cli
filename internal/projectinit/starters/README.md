# Init Starters

`volcano init` is built from embedded starter directories under
`internal/projectinit/starters`. Each starter is copied as a filesystem overlay
with managed-file conflict handling.

## Resolution Model

Every init run applies the `base` starter first. If no template is provided,
only the `base` starter is applied (environment files, migrations directory,
and README — no language-specific files).

When a template is provided, the CLI resolves it to a starter directory name and
passes that concrete name to `projectinit`:

```text
volcano init                         -> base only
volcano init nextjs                  -> base + nextjs
volcano init js                      -> base + javascript
volcano init python                  -> base + python
volcano init ruby                    -> base + ruby
volcano init nextjs --example notes  -> base + nextjs-notes
volcano init js --example hello-world -> base + javascript-hello-world
```

`projectinit` does not know about languages, aliases, or examples. It only loads
the starter directory name it is given. Keep alias and example-name resolution in
`internal/cmd/init/init.go`.

## Directory Naming

Use these names for bare-minimum templates:

```text
<template>
```

Use these names for examples:

```text
<template>-<example>
```

Examples:

```text
nextjs
nextjs-notes
javascript
javascript-hello-world
python
python-hello-world
ruby
ruby-hello-world
```

Template and example names should be lowercase, hyphen-separated, and stable.
Prefer aliases in `internal/cmd/init/init.go` over adding duplicate starter
directories.

## Starter Contents

The `base` starter owns common Volcano files:

```text
volcano/.gitignore
volcano/README.md
volcano/volcano.env
volcano/volcano.env.example
volcano/migrations/README.md
```

Template and example starters should only add files specific to that scaffold.
Do not duplicate base files unless the scaffold intentionally needs different
content and tests cover the conflict/idempotency behavior.

## Configuration Manifests

Only include `volcano/volcano-config.yaml` when the starter has at least one
real config resource to manage. An empty manifest like this is invalid:

```yaml
version: 1
functions: []
```

If a starter does not create a valid config manifest, `volcano init` must not
print `volcano config deploy` as a next step. The current implementation
prints that step only when the init result includes `volcano/volcano-config.yaml`
or `volcano-config.yaml`.

## Environment Files

`base/volcano/.gitignore` must keep `volcano.env` ignored. `volcano.env` may
contain local keys and edited environment values. Users should commit
`volcano.env.example` instead.

If a future starter creates additional local-only env files, add them to the
appropriate `.gitignore` in that starter.

## Adding A Starter

1. Create `internal/projectinit/starters/<name>/` with the exact files that
   should be written into a generated project.
2. For bare templates, use `<template>` as the starter directory name.
3. For examples, use `<template>-<example>` as the starter directory name.
4. Add or update CLI aliases in `internal/cmd/init/init.go` if users need a
   shorthand such as `js` for `javascript`.
5. Add tests in `internal/cmd/init/init_test.go` for CLI behavior and next-step
   output.
6. Add tests in `internal/projectinit/projectinit_test.go` for generated files,
   idempotency, and config/env edge cases when relevant.
7. Run `make lint`, `go test ./...`, and `go build ./...`.

## Safety Rules

- Keep starters local and static; do not fetch remote code during `init`.
- Do not include secrets, real tokens, or user-specific values.
- Do not add generated dependency directories such as `node_modules`, `.next`,
  `vendor/bundle`, `.venv`, or build outputs.
- Keep `volcano-cli` API-only. Starter changes must not import or depend on
  server internals.
