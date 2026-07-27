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
`VOLCANO_WEB_URL` (signup page only — `volcano login`'s browser flow opens the
backend's device-authorization verification URL directly and never uses this),
and `VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID`. `make local` also honors
`VOLCANO_IMAGE` from `.env.local` (mapped to `DEFAULT_LOCAL_IMAGE`, defaulting to
`kong/volcano:local-nightly`) so local dev builds bake the nightly local-mode
server image. An un-overridden `make build` bakes the default local-mode image
(currently `kong/volcano:local-nightly`, the only one volcano-hosting publishes);
pass `DEFAULT_LOCAL_IMAGE` to override it.
`VOLCANO_WEB_URL` is optional at
runtime too: when it isn't set, the CLI derives it from `VOLCANO_API_URL`
(`config.WebURL`) — a loopback API host maps to the conventional local Web port
3000, and an `api.` API host maps to the same host with that prefix stripped
(`api.volcano.dev` -> `volcano.dev`, `api.staging.volcano.dev` ->
`staging.volcano.dev`). Set `VOLCANO_WEB_URL` explicitly only when your backend
doesn't follow either convention.

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

Stable releases are managed by Release Please and are largely automatic. Only
`feat:` and `fix:` commits (and breaking changes) cut a release; `ci`, `chore`,
`docs`, `refactor`, `build`, `test`, `style`, `perf`, and `revert` do not.
Because merges to `main` are squash-only, Release Please reads the bump type
from the squash commit — which is the PR title for multi-commit PRs — so signal
a breaking change with a `feat!:` PR title or a `BREAKING CHANGE:` footer, not
just a `!` on a branch commit.

On each qualifying push to `main`, Release Please opens or updates a single
`release: <version>` PR that bumps `package.json` and
`.release-please-manifest.json` and updates `CHANGELOG.md`. What happens next
depends on the bump size:

- Minor and patch releases auto-merge once the required `check / check` run
  passes; the branch requires no approving review. The resulting SemVer tag
  triggers the publish workflow, which attaches signed binaries and publishes
  to npm with provenance.
- Major releases are left open for a maintainer to merge manually.

While the version is pre-1.0, breaking changes bump the minor
(`bump-minor-pre-major`), so `0.x` never jumps to `1.0.0` on its own. To set an
exact version deliberately (for example a real `1.0.0`), land a commit with a
`Release-As: <version>` footer and Release Please will use it.

Do not hand-edit `package.json`'s version or `.release-please-manifest.json`
outside the release PR: a CI guard rejects such changes on non-release PRs, the
`release-please--*` branch is writable only by the release app, and the publish
workflow rejects a stable tag whose `package.json` does not match the tag, or
that is not reachable from `origin/main`. Merging to `main` and creating
in-repo branches are restricted to the maintainer teams and the release app.

Prerelease and build metadata tags are not stable release tags. Nightly builds
remain automated from `main` and publish to the mutable `nightly` GitHub Release;
they are not npm releases and are separate from the stable Release Please flow.

Required repository secrets and variables for stable release publishing:

- `VOLCANO_APP_ID` (variable) and `VOLCANO_APP_KEY` (secret): the GitHub App
  used by Release Please to mint a short-lived installation token. A non-default
  token is required so the release tag/release triggers the publish workflow.
  The app must be installed on this repo with Contents and Pull requests
  read/write.
- `VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION`

Release assets include platform binaries, adjacent `.sigstore.json` bundles,
`install.sh`, `install.sh.sigstore.json`, and `SHA256SUMS`. The installer
downloads from GitHub Release assets and defaults to the latest stable release:

```bash
curl -fsSL https://github.com/Kong/volcano-cli/releases/latest/download/install.sh | bash
```

Set `VOLCANO_VERSION=vMAJOR.MINOR.PATCH` to install a pinned release.
