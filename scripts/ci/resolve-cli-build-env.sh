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
    STABLE_SEMVER_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
    NIGHTLY_SEMVER_RE='^v0\.0\.[0-9]+-nightly\.[0-9]{8}\.[0-9]+$'
    if [[ ! "$REF_NAME" =~ $STABLE_SEMVER_RE ]] && [[ ! "$REF_NAME" =~ $NIGHTLY_SEMVER_RE ]]; then
      echo "Unsupported CLI release tag: $REF_NAME"
      echo "Release tags must use stable SemVer form vMAJOR.MINOR.PATCH or nightly form v0.0.N-nightly.YYYYMMDD.NUMBER."
      exit 1
    fi
    CLI_DEFAULT_API_URL="https://api.volcano.dev"
    CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${PRODUCTION_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION:-}}"
    REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION"
    CLI_VERSION="${CLI_VERSION:-$REF_NAME}"
    ;;
  refs/heads/main)
    CLI_DEFAULT_API_URL="https://api.volcano.dev"
    CLI_FIRST_PARTY_DEVICE_CLIENT_ID="${PRODUCTION_FIRST_PARTY_DEVICE_CLIENT_ID:-${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION:-}}"
    REQUIRED_DEVICE_CLIENT_ID_VAR="VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION"
    if [ -z "${CLI_VERSION:-}" ]; then
      DATE_PART="$(date -u +%Y%m%d)"
      LATEST_STABLE_PATCH="$({
        git tag --list 'v0.0.*' || true
      } | awk '/^v0\.0\.[0-9]+$/ { sub(/^v0\.0\./, "", $0); print $0 }' | sort -n | tail -1)"
      NEXT_PATCH=$(( ${LATEST_STABLE_PATCH:-0} + 1 ))
      NIGHTLY_PREFIX="v0.0.${NEXT_PATCH}-nightly.${DATE_PART}."
      LATEST_NIGHTLY_NUMBER=0
      while IFS= read -r tag; do
        suffix="${tag#${NIGHTLY_PREFIX}}"
        if [[ "$suffix" =~ ^[0-9]+$ ]] && (( 10#$suffix > LATEST_NIGHTLY_NUMBER )); then
          LATEST_NIGHTLY_NUMBER=$((10#$suffix))
        fi
      done < <(git tag --list "${NIGHTLY_PREFIX}*")
      CLI_VERSION="${NIGHTLY_PREFIX}$((LATEST_NIGHTLY_NUMBER + 1))"
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
  echo "CLI_FIRST_PARTY_DEVICE_CLIENT_ID=${CLI_FIRST_PARTY_DEVICE_CLIENT_ID}"
  echo "CLI_VERSION=${CLI_VERSION}"
  echo "BUILD_DATE=${BUILD_DATE}"
} >> "$GITHUB_ENV"
