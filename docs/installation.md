# Installation

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

## Staging channel

The staging channel ships a build compiled against the Volcano staging
environment (`https://api.staging.volcano.dev`) for internal testing. Staging
builds install as `volcano-staging`, so they coexist with a production
`volcano` install, and `volcano-staging --version` reports the environment it
targets.

Install the staging build with Homebrew:

```bash
brew install Kong/volcano/volcano-staging
volcano-staging --version
```

Or with the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/Kong/volcano-cli/main/scripts/install-volcano.sh | VOLCANO_VERSION=staging sh
volcano-staging --version
```

Staging is published only as a signature-verified prerelease and is never the
`latest` release. npm/pnpm/yarn/bun staging installs are not yet available (a
staging dist-tag on the shared package would overwrite a production `volcano`
install); use Homebrew or the install script for now.

## Upgrading

`volcano upgrade` upgrades the CLI the same way it was installed: it delegates
to the package manager it came from (`npm`/`pnpm`/`yarn`/`bun install -g`, or
`brew upgrade`) and only replaces the binary in place for the manual
install.sh method. If the package manager isn't on your `PATH`, it prints the
command to run instead. The install method is recorded at install time (with a
fallback to the binary's path), so no configuration is needed.

Staging builds stay on the staging channel: a Homebrew `volcano-staging` install
upgrades with `brew upgrade volcano-staging`, and `volcano upgrade` on any other
staging build re-runs the staging installer rather than replacing it with a
production `latest` binary.

The npm package is a thin wrapper: its `postinstall` step downloads the
platform-specific binary from the matching GitHub Release and verifies it
against that release's `SHA256SUMS`. Set `VOLCANO_SKIP_DOWNLOAD=1` to skip the
download; the binary is fetched on first run instead.

If pnpm reports `ERR_PNPM_NO_GLOBAL_BIN_DIR`, run `pnpm setup`, restart your
shell, and retry the install.
