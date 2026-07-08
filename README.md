# volcano-cli

`volcano` is the command-line client for Volcano, Kong's hosting platform.
It provides local development workflows and hosted API commands from a
standalone Go CLI.

## Quickstart

Install the latest published release:

```bash
curl -fsSL https://github.com/Kong/volcano-cli/releases/latest/download/install.sh | bash
```

Or install from npm:

```bash
npm install -g @volcano.dev/cli
volcano --help
```

The npm package is a thin wrapper: its `postinstall` step downloads the
platform-specific binary from the matching GitHub Release and verifies it
against that release's `SHA256SUMS`. Set `VOLCANO_SKIP_DOWNLOAD=1` to skip the
download (it is fetched on first run instead).

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

## Authentication

New to Volcano? Create an account from the CLI:

```bash
volcano signup
```

`volcano signup` prefills your email from `git config --global user.email`
when available (press Enter to accept, or type a different address), then
opens Volcano's web signup flow in your browser. Once you finish in the
browser, the CLI completes the device-authorization handshake and saves your
credentials to `~/.volcano/config.json` — so a single command signs you up
**and** logs you in.

Already have an account? Authenticate with `volcano login`:

```bash
# Browser-based login (default)
volcano login

# Token-based login (for CI/CD)
volcano login --token pk-xxxxxxxxxx

# Or skip login entirely with an environment variable
export VOLCANO_TOKEN=pk-xxxxxxxxxx
```

Log out at any time (this deletes local credentials but does not revoke the
token — revoke it in the Volcano dashboard to fully cut off access):

```bash
volcano logout
```

To target a non-production environment, set `VOLCANO_API_URL` to that backend's
API endpoint. `volcano signup` then opens the signup page on the **same**
environment the API points at — it follows the device-flow verification URL,
just like `volcano login` — so you normally don't need anything else. Set
`VOLCANO_WEB_URL` only to force a specific web origin.

## Project configuration (`volcano-config.yaml`)

`volcano config deploy` uploads a declarative manifest
(`volcano/volcano-config.yaml` or `./volcano-config.yaml`) to the server, which
validates and reconciles the full project configuration — project settings,
database assertions, variables, buckets and policies, realtime, the complete
auth configuration (providers, email, templates, managed pages), function
visibility and schedulers, and frontend custom domains. The same manifest
applies to local mode and cloud. `volcano config pull` downloads the current
configuration as a canonical manifest rendered by the server.

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

Key semantics (see the hosted manifest reference for the full schema):

- **Declared config sections are the source of truth.** Variables, bucket
  policies, OAuth providers, email templates, and function schedulers are
  fully synced when declared: entries absent from the manifest are deleted.
  Omitted sections and fields keep their server values.
- **Functions, frontends, databases, and buckets are never created or deleted
  through the manifest** — only their configuration is updated. A manifest
  entry for a resource that does not exist is skipped with a warning; a
  deployed resource missing from a declared section is reported too.
- `${ENV_VAR}` references are interpolated before upload; a reference to an
  unset variable is an error, and `$$` produces a literal `$`.
- `volcano config deploy --dry-run` prints the projected actions without
  changing anything. Validation failures (including plan-gate violations)
  exit non-zero with the server's error list, and nothing is applied.
- Write-only secrets (SMTP password, OAuth client secrets, TLS material) are
  omitted from `config pull` exports; keep them in your environment and set
  them via `${ENV_VAR}` interpolation.

Behavior changes from older CLI releases: buckets are no longer auto-created,
an omitted `policies` key now leaves a bucket's policies untouched (previously
it deleted them all), schedulers are now deleted by omission within a declared
`schedulers` list, and the scheduler `regions` field is no longer supported
(placement is managed by the server).

## Contributing

See `CONTRIBUTING.md` for local workflows, generated-code guidance, release
notes, and pull request expectations. Participation is governed by
`CODE_OF_CONDUCT.md`.

If you believe you have found a security vulnerability, do not open a public
issue. Follow `SECURITY.md` instead.

## License

Volcano CLI is licensed under the Apache License, Version 2.0. See `LICENSE`.
