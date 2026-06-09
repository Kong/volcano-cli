# Local Image Refactor CLI Plan

This document tracks CLI-side work needed to support the hosting repository split into separate local and cloud server images.

## Goal

The CLI should run the local development stack against the local-only server image.

- Local server image: `kong/volcano:local-nightly`
- Cloud server image: `kong/volcano:cloud-nightly`

The CLI should continue to support `VOLCANO_IMAGE` overrides so developers and CI can pin a digest or test image.

## Current Behavior

- `internal/localmode/service.go` defaults `defaultVolcanoImage` to `kong/volcano:nightly`.
- `internal/localmode/compose.go` resolves the final image in this order: default, `.env.local`, process `VOLCANO_IMAGE`.
- `internal/localmode/assets/docker-compose.template.yml` has a fallback `${VOLCANO_IMAGE:-kong/volcano:latest}`, but Go injects a resolved `VOLCANO_IMAGE`, so the template fallback is rarely used.
- `internal/localmode/info.go` expects `/app/volcano-hosting local info --format json` inside the `volcano-server` container.
- No version handshake exists between CLI and server image.

## Supported Local Feature Set

The local image must support the command surface currently exposed under local mode:

- `volcano start`, `volcano status`, `volcano stop`, `volcano restart`
- `volcano local databases`
- `volcano local functions`
- `volcano local functions schedulers`
- `volcano local storage`
- `volcano local variables`
- `volcano local config deploy`
- `volcano local migrations`
- `volcano local reset`

Not currently exposed under `volcano local`:

- Frontends
- Billing, credits, add-ons, plans, Stripe webhooks

## Checklist

- [ ] Update `internal/localmode/service.go` default image to `kong/volcano:local-nightly`.
- [ ] Update `internal/cmd/localmode/localmode.go` help text to show `VOLCANO_IMAGE=kong/volcano:local-nightly volcano start`.
- [ ] Update `internal/localmode/assets/docker-compose.template.yml` fallback image to `kong/volcano:local-nightly` for consistency.
- [ ] Confirm the asset generation source, if any, is updated before editing generated assets.
- [ ] Update local-mode tests that assert or default the old image tag.
- [ ] Update e2e defaults in `tests/e2e/localmode/localmode_test.go` from `kong/volcano:nightly` to `kong/volcano:local-nightly`.
- [ ] Update `.github/workflows/check.yml` pinned image tag/digest once the new local image is published.
- [ ] Document `VOLCANO_IMAGE` override in command help and project docs.
- [ ] Keep `.env.local` override behavior unchanged.
- [ ] Keep process `VOLCANO_IMAGE` taking precedence over `.env.local`.
- [ ] Keep container names unchanged: `volcano-server`, `volcano-postgres`, `volcano-redis`.
- [ ] Keep compose project name unchanged: `volcano`.
- [ ] Keep server binary path unchanged: `/app/volcano-hosting`.
- [ ] Keep `local info --format json` contract unchanged.
- [ ] Consider adding a future compatibility field to local info JSON, but do not require it for the image split unless the server contract changes.

## Verification Checklist

- [ ] `go test ./internal/localmode/...`
- [ ] `go test ./internal/cmd/localmode/...`
- [ ] `go test ./...`
- [ ] `VOLCANO_IMAGE=kong/volcano:local-nightly volcano start`
- [ ] `volcano status`
- [ ] `volcano local databases list`
- [ ] `volcano local functions list`
- [ ] `volcano local storage bucket list`
- [ ] `volcano stop`

## Coordination With Hosting

- The hosting repo must publish `kong/volcano:local-nightly` from `Dockerfile` built with `go build -tags local`.
- The hosting repo must publish `kong/volcano:cloud-nightly` from `Dockerfile.server` built without local tags.
- The local image must not contain AWS SDK or Stripe SDK dependencies.
- The local image must keep `/app/volcano-hosting local info --format json` stable.
- If hosting changes local info JSON, add CLI-side compatibility handling in `internal/localmode/info.go`.
