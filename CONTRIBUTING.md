# Contributing To Volcano CLI

## Local prerequisites

- Go (use the toolchain version declared in `go.mod`).
- `make`.

That is the entire standalone CLI toolchain. `golangci-lint` is pinned via
a `tool` directive in `go.mod` and runs through `go tool golangci-lint`, so
no separate install step is required.

## Common workflows

| Goal                          | Command                                  |
| ----------------------------- | ---------------------------------------- |
| Build the binary              | `make build`                             |
| Build against a dev backend   | `make local`                             |
| Run tests                     | `make test`                              |
| Lint (golangci-lint)          | `make lint`                              |
| Lint + test                   | `make check`                             |
| Tidy module dependencies      | `make tidy`                              |
| Local-mode smoke test         | `make localmode-e2e`                     |

`make local` builds the binary with the compiled-in defaults loaded from a
gitignored `.env.local` file, so you can point the CLI at a non-production
backend without exporting variables each time. Supported keys: `VOLCANO_API_URL`,
`VOLCANO_WEB_URL` (signup/login pages), and `VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID`.
When only a loopback `VOLCANO_API_URL` is set, `VOLCANO_WEB_URL` defaults to
`http://localhost:3000`.

`make localmode-e2e` uses Docker and is intentionally heavier than the normal
unit-test workflow. Run it when changing local-mode startup, reset, health, or
Docker integration behavior.

## Generated code

`internal/apiclient/client.gen.go` is generated and checked in here. Never
hand-edit it.

If a CLI change requires generated types or methods, coordinate with the
maintainers. They will update the API contract and refresh the checked-in
client output as part of the change.

## Init scaffolds

`volcano init` starter templates live under `internal/projectinit/starters` and
are embedded into the CLI. See `internal/projectinit/starters/README.md` before
adding or changing init scaffolds, especially for naming, `--example` handling,
config manifests, ignored env files, and required tests.

## API-only boundary

Imports from Volcano server internals are not allowed. If you need new hosted
behavior, open an issue or pull request describing the API capability the CLI
needs.

Keep CLI-side validation focused on local filesystem/process safety, local
configuration correctness, or avoiding requests that the generated client cannot
represent. Hosted validation, authorization, limits, defaulting, pagination
semantics, and resource-state decisions belong in the hosted API.

## Contributor License Agreement

Kong may require external contributors to sign a contributor license agreement
before their changes can be merged. When the repository CLA check is enabled,
the pull request check will provide signing instructions.

## Pull requests

- Open PRs as drafts.
- Use Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`, ...).
- Keep PRs focused — one command, bug fix, or cohesive group per PR.
- Include tests for behavior changes.
- Run `make lint`, `go test ./...`, and `go build ./...` before pushing when
  code changes are included.

Security vulnerabilities should not be reported through public issues or pull
requests. Follow `SECURITY.md` instead.

## Maintainer Release Notes

Publishing uses GitHub Releases for artifact hosting and GitHub OIDC for
Sigstore signing. Do not configure long-lived AWS secrets for CLI releases.

The publish workflow builds signed binaries for `linux-amd64`, `linux-arm64`,
`macos-amd64`, `macos-arm64`, and `windows-amd64`. It publishes stable release
assets from SemVer tags.

To cut a stable release, merge the release commit to `main`, tag that commit
with a SemVer tag such as `v1.2.3`, and push the tag. Prerelease and build
metadata tags are not stable release tags. The publish workflow also rejects
stable release tags that are not reachable from `origin/main`.

Required repository variable for stable release publishing:

- `VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION`

Release assets include platform binaries, adjacent `.sigstore.json` bundles,
`install.sh`, `install.sh.sigstore.json`, and `SHA256SUMS`. The installer
downloads from GitHub Release assets and defaults to the latest stable release:

```bash
curl -fsSL https://github.com/Kong/volcano-cli/releases/latest/download/install.sh | bash
```

Set `VOLCANO_VERSION=vMAJOR.MINOR.PATCH` to install a pinned release.
