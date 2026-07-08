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

More detail lives in `docs/`:

- [Installation details](docs/installation.md)
- [Authentication](docs/authentication.md)
- [Project configuration](docs/project-configuration.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for local workflows, generated-code
guidance, release notes, and pull request expectations. Participation is
governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

If you believe you have found a security vulnerability, do not open a public
issue. Follow [SECURITY.md](SECURITY.md) instead.

## License

Volcano CLI is licensed under the Apache License, Version 2.0. See
[LICENSE](LICENSE).
