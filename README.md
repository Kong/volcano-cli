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
npm install -g @kong/volcano-cli
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

Reconciliation follows one rule that mirrors the server: **fields you declare
are enforced; fields you omit are left server-managed.** An omitted `enabled`,
`payload`, or `regions` keeps whatever the scheduler already has on the server
(on first create the server applies its defaults — `enabled: true`, an empty
payload, and one chosen region). In particular, `config deploy` will not
re-enable a scheduler you disabled out of band unless the manifest sets
`enabled: true`. `cron` is always required and enforced.

Reconciliation is also **non-destructive**: it creates and updates the
schedulers a function declares (matched by `name`, preserving the scheduler ID)
but never deletes or disables one. A scheduler the manifest no longer declares
is left running; to remove or disable one, use the imperative commands
(`volcano functions schedulers delete` / `disable`).

## Contributing

See `CONTRIBUTING.md` for local workflows, generated-code guidance, release
notes, and pull request expectations. Participation is governed by
`CODE_OF_CONDUCT.md`.

If you believe you have found a security vulnerability, do not open a public
issue. Follow `SECURITY.md` instead.

## License

Volcano CLI is licensed under the Apache License, Version 2.0. See `LICENSE`.
