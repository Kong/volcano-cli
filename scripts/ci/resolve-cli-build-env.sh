#!/usr/bin/env bash
set -euo pipefail

REF="${REF:-${GITHUB_REF:-}}"
REF_NAME="${REF_NAME:-${GITHUB_REF_NAME:-}}"

if [ -z "$REF" ]; then
  echo "REF or GITHUB_REF is required"
  exit 1
fi
if [ -z "${GITHUB_ENV:-}" ]; then
  echo "GITHUB_ENV is required"
  exit 1
fi

# ponytail: staging default for the whole testing phase so every release path
# (including stable tags -> `latest`/npm/brew) builds against staging; restore
# these two lines and each per-case block below before GA
# CLI_DEFAULT_API_URL="https://api.volcano.dev"
# CLI_DEFAULT_WEB_URL="https://volcano.dev"
CLI_DEFAULT_API_URL="https://api.staging.volcano.dev"
CLI_DEFAULT_WEB_URL="https://staging.volcano.dev"
CLI_FIRST_PARTY_DEVICE_CLIENT_ID=""
# Local-mode server image baked into the release binary's `volcano start` default.
# All channels currently ship kong/volcano:local-nightly — the only local-mode
# image volcano-hosting publishes (no stable release cut, so local-latest does
# not exist yet).
# ponytail: flip stable SemVer tags to kong/volcano:local-latest once a stable
# volcano-hosting release publishes it (see the refs/tags/* case below).
CLI_DEFAULT_LOCAL_IMAGE="kong/volcano:local-nightly"

case "$REF" in
  refs/tags/*)
    if [ -z "$REF_NAME" ]; then
      REF_NAME="${REF#refs/tags/}"
    fi
    STABLE_SEMVER_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
    NIGHTLY_SEMVER_RE='^v0\.0\.[0-9]+-nightly\.[0-9]{8}\.[0-9]+$'
    if [[ ! "$REF_NAME" =~ $STABLE_SEMVER_RE ]] && [[ ! "$REF_NAME" =~ $NIGHTLY_SEMVER_RE ]]; then
      echo "Unsupported CLI release tag: $REF_NAME"
      echo "Release tags must use stable SemVer form vMAJOR.MINOR.PATCH or nightly form v0.0.N-nightly.YYYYMMDD.NUMBER."
      exit 1
    fi
    # ponytail: stable tags build against staging during the testing phase so
    # `latest`/npm default to staging; restore these four lines before GA
    # CLI_DEFAULT_API_URL="https://api.volcano.dev"
    # CLI_DEFAULT_WEB_URL="https://volcano.dev"
    # CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${PRODUCTION_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION:-}}"
    # REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION"
    CLI_DEFAULT_API_URL="https://api.staging.volcano.dev"
    CLI_DEFAULT_WEB_URL="https://staging.volcano.dev"
    CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${STAGING_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_STAGING:-}}"
    REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_STAGING"
    # ponytail: stable SemVer tags should flip to kong/volcano:local-latest here
    # once volcano-hosting publishes it; until then every channel uses the
    # top-level kong/volcano:local-nightly (the only published local-mode image).
    CLI_VERSION="$REF_NAME"
    ;;
  refs/heads/main)
    # ponytail: staging default for the testing phase (nightly); restore these four lines before GA
    # CLI_DEFAULT_API_URL="https://api.volcano.dev"
    # CLI_DEFAULT_WEB_URL="https://volcano.dev"
    # CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${PRODUCTION_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION:-}}"
    # REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION"
    CLI_DEFAULT_API_URL="https://api.staging.volcano.dev"
    CLI_DEFAULT_WEB_URL="https://staging.volcano.dev"
    CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${STAGING_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_STAGING:-}}"
    REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_STAGING"
    if [ -z "${CLI_VERSION:-}" ]; then
      echo "CLI_VERSION is required for main release builds. The publish workflow must pre-resolve the nightly version."
      exit 1
    fi
    ;;
  *)
    echo "Unsupported ref for CLI release build: $REF"
    echo "Release builds must run from main, stable SemVer tags such as refs/tags/v1.2.3, or nightly tags such as refs/tags/v0.0.8-nightly.20260618.1."
    exit 1
    ;;
esac

if [ -z "$CLI_FIRST_PARTY_DEVICE_CLIENT_ID" ]; then
  echo "Missing first-party device client ID for CLI build."
  echo "Set ${REQUIRED_DEVICE_CLIENT_ID_VAR} repository variable."
  exit 1
fi

BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

{
  echo "CLI_DEFAULT_API_URL=${CLI_DEFAULT_API_URL}"
  echo "CLI_DEFAULT_WEB_URL=${CLI_DEFAULT_WEB_URL}"
  echo "CLI_DEFAULT_LOCAL_IMAGE=${CLI_DEFAULT_LOCAL_IMAGE}"
  echo "CLI_FIRST_PARTY_DEVICE_CLIENT_ID=${CLI_FIRST_PARTY_DEVICE_CLIENT_ID}"
  echo "CLI_VERSION=${CLI_VERSION}"
  echo "BUILD_DATE=${BUILD_DATE}"
} >> "$GITHUB_ENV"
