# volcano-cli

`volcano` is the command-line client for Volcano, Kong's hosting platform.
It provides local development workflows and hosted API commands from a
standalone Go CLI.

## Quickstart

Install the latest published release:

```bash
curl -fsSL https://github.com/Kong/volcano-cli/releases/latest/download/install.sh | bash
```

Build from source:

```bash
make build
./volcano --help
./volcano --version
make test
```

From the CLI checkout, create an empty sibling project directory and run it:

```bash
VOLCANO_CLI="$(pwd)/volcano"
mkdir ../volcano-quickstart
cd ../volcano-quickstart
"$VOLCANO_CLI" init javascript
"$VOLCANO_CLI" start
"$VOLCANO_CLI" variables deploy
"$VOLCANO_CLI" functions deploy --all
"$VOLCANO_CLI" config deploy
"$VOLCANO_CLI" migrations deploy --all -d app
```

`volcano init` without a template creates a base scaffold (environment
files, migrations directory, and README). Use a template to add
language-specific files: `javascript` (aliases: `js`, `node`, `nodejs`),
`nextjs`, `python`, or `ruby`.

## Project configuration (`volcano-config.yaml`)

`volcano config deploy` reconciles declarative project configuration
(`volcano/volcano-config.yaml` or `./volcano-config.yaml`) against the active
target — the same manifest applies to local mode and cloud.

Functions may declare scheduled invocations. `name` and `cron` are required;
`enabled` (default `true`), `payload`, and `regions` are optional. A function
entry is valid if it sets `public` **or** declares at least one scheduler.

```yaml
version: 1
functions:
  - name: hello
    public: false
    schedulers:
      - name: refresh-cache      # required, unique per function (the reconcile key)
        cron: "*/5 * * * *"
        enabled: true
        payload: { job: refresh }
        regions: [us-east-1]     # omit to let the server pick one deployed region
```

Scheduler reconciliation is **non-destructive**: `config deploy` creates and
updates the schedulers a function declares (matched by `name`, preserving the
scheduler ID) but never deletes one. Removing a scheduler from the manifest is
a no-op — delete it explicitly with `volcano functions schedulers delete`.
Likewise, `cron`/`payload`/`enabled` are reconciled, but `regions` are only
enforced when declared (an omitted `regions` is left server-managed).

## Contributing

See `CONTRIBUTING.md` for local workflows, generated-code guidance, release
notes, and pull request expectations. Participation is governed by
`CODE_OF_CONDUCT.md`.

If you believe you have found a security vulnerability, do not open a public
issue. Follow `SECURITY.md` instead.

## License

Volcano CLI is licensed under the Apache License, Version 2.0. See `LICENSE`.
