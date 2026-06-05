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

CLI_DEFAULT_API_URL="https://api.volcano.dev"
CLI_FIRST_PARTY_DEVICE_CLIENT_ID=""

case "$REF" in
  refs/tags/*)
    if [ -z "$REF_NAME" ]; then
      REF_NAME="${REF#refs/tags/}"
    fi
    VERSION="${REF_NAME#v}"
    STABLE_SEMVER_RE='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
    if [ "$REF_NAME" = "$VERSION" ] || [[ ! "$VERSION" =~ $STABLE_SEMVER_RE ]]; then
      echo "Unsupported CLI release tag: $REF_NAME"
      echo "Release tags must use stable SemVer form vMAJOR.MINOR.PATCH, for example v1.2.3."
      echo "Prerelease and build metadata tags are not production release tags."
      exit 1
    fi
    CLI_DEFAULT_API_URL="https://api.volcano.dev"
    CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${PRODUCTION_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION:-}}"
    REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION"
    CLI_VERSION="$REF_NAME"
    ;;
  *)
    echo "Unsupported ref for CLI release build: $REF"
    echo "Release builds must run from stable SemVer tags such as refs/tags/v1.2.3."
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
  echo "CLI_FIRST_PARTY_DEVICE_CLIENT_ID=${CLI_FIRST_PARTY_DEVICE_CLIENT_ID}"
  echo "CLI_VERSION=${CLI_VERSION}"
  echo "BUILD_DATE=${BUILD_DATE}"
} >> "$GITHUB_ENV"
