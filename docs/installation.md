# Installation

Install the latest published release:

```bash
curl -fsSL https://github.com/Kong/volcano-cli/releases/latest/download/install.sh | bash
```

Or install with Homebrew:

```bash
brew install kong-volcano/tap/volcano
volcano --help
```

Or install from npm-compatible package managers:

```bash
npm install -g @volcano.dev/cli
pnpm add -g @volcano.dev/cli
bun add -g @volcano.dev/cli
volcano --help
```

The npm package is a thin wrapper: its `postinstall` step downloads the
platform-specific binary from the matching GitHub Release and verifies it
against that release's `SHA256SUMS`. Set `VOLCANO_SKIP_DOWNLOAD=1` to skip the
download; the binary is fetched on first run instead.

If pnpm reports `ERR_PNPM_NO_GLOBAL_BIN_DIR`, run `pnpm setup`, restart your
shell, and retry the install.
