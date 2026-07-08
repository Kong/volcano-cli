# Installation

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
download; the binary is fetched on first run instead.

Build from source:

```bash
make build
./volcano --help
./volcano --version
make test
```
