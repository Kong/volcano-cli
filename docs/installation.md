---
title: "Installation"
description: "Install the Volcano CLI with npm, pnpm, Bun, Homebrew, or the install script."
---

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

## Upgrading

`volcano upgrade` upgrades the CLI the same way it was installed: it delegates
to the package manager it came from (`npm`/`pnpm`/`yarn`/`bun install -g`, or
`brew upgrade`) and only replaces the binary in place for the manual
install.sh method. If the package manager isn't on your `PATH`, it prints the
command to run instead. The install method is recorded at install time (with a
fallback to the binary's path), so no configuration is needed.

The npm package is a thin wrapper: its `postinstall` step downloads the
platform-specific binary from the matching GitHub Release and verifies it
against that release's `SHA256SUMS`. Set `VOLCANO_SKIP_DOWNLOAD=1` to skip the
download; the binary is fetched on first run instead.

If pnpm reports `ERR_PNPM_NO_GLOBAL_BIN_DIR`, run `pnpm setup`, restart your
shell, and retry the install.
