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
environment (`https://api.staging.volcano.dev`) for internal testing. It is the
same `volcano` command and the same `@volcano.dev/cli` package as production —
you deliberately choose which environment to install, and `volcano --version`
reports the environment the build targets. A staging install replaces a
production one on the same machine (and vice versa); set `VOLCANO_API_URL` at
runtime if you need to reach both at once.

Install the staging build with npm (or `pnpm add -g`, `yarn global add`, `bun add -g`):

```bash
npm install -g @volcano.dev/cli@staging
volcano --version
```

With Homebrew:

```bash
brew install Kong/volcano/volcano-staging
volcano --version
```

Or with the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/Kong/volcano-cli/main/scripts/install-volcano.sh | VOLCANO_VERSION=staging sh
volcano --version
```

Switch back to production by installing `@volcano.dev/cli` (`@latest`), the
`volcano` Homebrew formula, or running the install script without
`VOLCANO_VERSION`. Staging is published only as a signature-verified prerelease
and is never the npm `latest` or the GitHub `latest` release.

## Upgrading

`volcano upgrade` upgrades the CLI the same way it was installed: it delegates
to the package manager it came from (`npm`/`pnpm`/`yarn`/`bun install -g`, or
`brew upgrade`) and only replaces the binary in place for the manual
install.sh method. If the package manager isn't on your `PATH`, it prints the
command to run instead. The install method is recorded at install time (with a
fallback to the binary's path), so no configuration is needed.

Staging installs stay on the staging channel: `volcano upgrade` re-installs
`@volcano.dev/cli@staging` (npm/pnpm/yarn/bun), runs `brew upgrade
volcano-staging`, or (for install-script builds) points you back at the staging
installer — it never silently replaces a staging build with a production
`latest` binary.

The npm package is a thin wrapper: its `postinstall` step downloads the
platform-specific binary from the matching GitHub Release and verifies it
against that release's `SHA256SUMS`. Set `VOLCANO_SKIP_DOWNLOAD=1` to skip the
download; the binary is fetched on first run instead.

If pnpm reports `ERR_PNPM_NO_GLOBAL_BIN_DIR`, run `pnpm setup`, restart your
shell, and retry the install.
