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
"$VOLCANO_CLI" init
"$VOLCANO_CLI" start
"$VOLCANO_CLI" local variables deploy
"$VOLCANO_CLI" local functions deploy --all
"$VOLCANO_CLI" local config deploy
"$VOLCANO_CLI" local migrations deploy --all -d app
```

`volcano init` also supports starter templates such as `nextjs`, `js`,
`python`, and `ruby`.

## Contributing

See `CONTRIBUTING.md` for local workflows, generated-code guidance, release
notes, and pull request expectations. Participation is governed by
`CODE_OF_CONDUCT.md`.

If you believe you have found a security vulnerability, do not open a public
issue. Follow `SECURITY.md` instead.

## License

Volcano CLI is licensed under the Apache License, Version 2.0. See `LICENSE`.
