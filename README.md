# Volcano CLI

Volcano is a hosting platform for building applications with functions,
databases, and authentication. Deploy backend logic, provision PostgreSQL
databases, and add user authentication—all through a unified API. The `volcano`
CLI helps you scaffold, run, and manage Volcano projects from your terminal.

## Quickstart

Install with npm:

```bash
npm install -g @volcano.dev/cli
volcano --help
```

Or install with pnpm:

```bash
pnpm add -g @volcano.dev/cli
volcano --help
```

Or install with Bun:

```bash
bun add -g @volcano.dev/cli
volcano --help
```

Or install with Homebrew:

```bash
brew install Kong/volcano/volcano
volcano --help
```

Or install manually:

```bash
curl -fsSL https://github.com/Kong/volcano-cli/releases/latest/download/install.sh | bash
volcano --help
```

Create a project directory and start local development:

```bash
mkdir volcano-quickstart
cd volcano-quickstart
volcano init javascript
volcano start
volcano variables deploy
volcano functions deploy --all
volcano config deploy
volcano migrations deploy --all -d app
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
guidance, building from source, release notes, and pull request expectations.
Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

If you believe you have found a security vulnerability, do not open a public
issue. Follow [SECURITY.md](SECURITY.md) instead.

## License

Volcano CLI is licensed under the Apache License, Version 2.0. See
[LICENSE](LICENSE).
