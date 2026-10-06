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
curl -fsSL https://download.volcano.dev/builds/install.sh | sh
volcano --help
```

To install the CLI and set up Volcano in detected coding agents:

```bash
curl -fsSL https://download.volcano.dev/builds/install.sh | sh -s -- --setup
```

The script requires `cosign` to verify the release signature. To select a specific
version, set `VOLCANO_VERSION` when running the installer. This example pins the
current release:

```bash
curl -fsSL https://download.volcano.dev/builds/install.sh -o install-volcano.sh
VOLCANO_VERSION="$(curl -fsSL https://download.volcano.dev/builds/releases/latest-version)" sh install-volcano.sh
```

The install script, the latest-version lookup, and the CLI and signature
downloads all use `download.volcano.dev`, and `cosign` can also contact Sigstore
for trust metadata. On a network that allows GitHub but not
`download.volcano.dev`, fetch the script from
`https://github.com/Kong/volcano-cli/releases/latest/download/install.sh` and set
`VOLCANO_GITHUB_RELEASES_URL=https://github.com/Kong/volcano-cli/releases`.
Versions released before Volcano downloads are only on
[GitHub Releases](https://github.com/Kong/volcano-cli/releases); to install one
with `VOLCANO_VERSION`, also set `VOLCANO_GITHUB_RELEASES_URL` to that URL.

## Windows downloads

Choose the executable for your Windows architecture:

- [Windows x64 (amd64)](https://download.volcano.dev/builds/releases/latest/download/volcano-windows-amd64.exe)
- [Windows ARM64](https://download.volcano.dev/builds/releases/latest/download/volcano-windows-arm64.exe)

Rename the downloaded file to `volcano.exe` and add its directory to your `PATH`. The npm, pnpm, and Bun installers select the executable for the architecture of your JavaScript runtime. Use an ARM64 runtime to install the native ARM64 executable.

For manual Windows installs, download the matching executable again to upgrade. Self-upgrade does not replace a running Windows executable.

## Upgrading

`volcano upgrade` upgrades the CLI the same way it was installed: it delegates
to the package manager it came from (`npm`/`pnpm`/`yarn`/`bun install -g`, or
`brew upgrade`) and only replaces the binary in place for the manual
install.sh method. If the package manager isn't on your `PATH`, it prints the
command to run instead. The install method is recorded at install time (with a
fallback to the binary's path), so no configuration is needed.

The npm package is a thin wrapper: its `postinstall` step downloads the
platform-specific binary from Volcano downloads and verifies it
against that release's `SHA256SUMS`. Set `VOLCANO_SKIP_DOWNLOAD=1` to skip the
download; the binary is fetched on first run instead.

If pnpm reports `ERR_PNPM_NO_GLOBAL_BIN_DIR`, run `pnpm setup`, restart your
shell, and retry the install.
