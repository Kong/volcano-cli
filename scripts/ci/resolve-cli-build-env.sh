#!/usr/bin/env bash
set -euo pipefail

REF="${REF:-${GITHUB_REF:-}}"
REF_NAME="${REF_NAME:-${GITHUB_REF_NAME:-}}"
# Build environment selector. Production is the default; staging is opt-in via a
# dedicated staging-v* release tag (see the channel cross-check below).
ENVIRONMENT="${ENVIRONMENT:-production}"

if [ -z "$REF" ]; then
  echo "REF or GITHUB_REF is required"
  exit 1
fi
if [ -z "${GITHUB_ENV:-}" ]; then
  echo "GITHUB_ENV is required"
  exit 1
fi

STABLE_SEMVER_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
NIGHTLY_SEMVER_RE='^v0\.0\.[0-9]+-nightly\.[0-9]{8}\.[0-9]+$'
STAGING_SEMVER_RE='^staging-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

# Resolve CLI_VERSION and the channel's expected environment from the ref. The
# ref (which trigger fired) is the source of truth for which environment a build
# belongs to; ENVIRONMENT is cross-checked against it below.
case "$REF" in
  refs/tags/*)
    if [ -z "$REF_NAME" ]; then
      REF_NAME="${REF#refs/tags/}"
    fi
    if [[ "$REF_NAME" =~ $STAGING_SEMVER_RE ]]; then
      REF_ENVIRONMENT="staging"
    elif [[ "$REF_NAME" =~ $STABLE_SEMVER_RE ]] || [[ "$REF_NAME" =~ $NIGHTLY_SEMVER_RE ]]; then
      REF_ENVIRONMENT="production"
    else
      echo "Unsupported CLI release tag: $REF_NAME"
      echo "Release tags must use stable SemVer form vMAJOR.MINOR.PATCH, nightly form v0.0.N-nightly.YYYYMMDD.NUMBER, or staging form staging-vMAJOR.MINOR.PATCH."
      exit 1
    fi
    CLI_VERSION="$REF_NAME"
    ;;
  refs/heads/main)
    REF_ENVIRONMENT="production"
    if [ -z "${CLI_VERSION:-}" ]; then
      echo "CLI_VERSION is required for main release builds. The publish workflow must pre-resolve the nightly version."
      exit 1
    fi
    ;;
  *)
    echo "Unsupported ref for CLI release build: $REF"
    echo "Release builds must run from main, stable SemVer tags such as refs/tags/v1.2.3, nightly tags such as refs/tags/v0.0.8-nightly.20260618.1, or staging tags such as refs/tags/staging-v1.2.3."
    exit 1
    ;;
esac

# The ref's channel is authoritative. A mismatched ENVIRONMENT would bake the
# wrong environment's URLs + device client id into a channel (e.g. a staging tag
# shipping production config, or vice versa), so fail hard instead.
if [ "$ENVIRONMENT" != "$REF_ENVIRONMENT" ]; then
  echo "ENVIRONMENT=${ENVIRONMENT} does not match the ${REF} channel (expected ${REF_ENVIRONMENT})."
  exit 1
fi

# Data-driven per-environment build values. Only the API URL + device client id
# differ between environments; the web URL follows the api.<env>.volcano.dev ->
# <env>.volcano.dev convention the CLI derives at runtime, and is set explicitly
# here so the compiled default matches.
case "$ENVIRONMENT" in
  production)
    CLI_DEFAULT_API_URL="https://api.volcano.dev"
    CLI_DEFAULT_WEB_URL="https://volcano.dev"
    CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${PRODUCTION_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION:-}}"
    REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION"
    ;;
  staging)
    CLI_DEFAULT_API_URL="https://api.staging.volcano.dev"
    CLI_DEFAULT_WEB_URL="https://staging.volcano.dev"
    CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${STAGING_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_STAGING:-}}"
    REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_STAGING"
    ;;
  *)
    echo "Unsupported ENVIRONMENT: ${ENVIRONMENT}; use production or staging."
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
  echo "CLI_FIRST_PARTY_DEVICE_CLIENT_ID=${CLI_FIRST_PARTY_DEVICE_CLIENT_ID}"
  echo "CLI_VERSION=${CLI_VERSION}"
  echo "BUILD_DATE=${BUILD_DATE}"
} >> "$GITHUB_ENV"
