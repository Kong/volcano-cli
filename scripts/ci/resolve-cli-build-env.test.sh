#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
output="$(mktemp)"
trap 'rm -f "$output"' EXIT

REF=refs/tags/v1.2.3 REF_NAME=v1.2.3 GITHUB_ENV="$output" \
  PRODUCTION_FIRST_PARTY_DEVICE_CLIENT_ID=prod-client \
  STAGING_FIRST_PARTY_DEVICE_CLIENT_ID=staging-client \
  "$root/scripts/ci/resolve-cli-build-env.sh"
grep -Fx 'CLI_DEFAULT_API_URL=https://api.volcano.dev' "$output"
grep -Fx 'CLI_DEFAULT_WEB_URL=https://volcano.dev' "$output"
grep -Fx 'CLI_FIRST_PARTY_DEVICE_CLIENT_ID=prod-client' "$output"

if error="$(REF=refs/tags/v1.2.3 REF_NAME=v1.2.3 GITHUB_ENV="$output" \
  PRODUCTION_FIRST_PARTY_DEVICE_CLIENT_ID= \
  VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION= \
  STAGING_FIRST_PARTY_DEVICE_CLIENT_ID=staging-client \
  "$root/scripts/ci/resolve-cli-build-env.sh" 2>&1)"; then
  echo 'release build accepted a staging-only device client ID' >&2
  exit 1
fi
[[ "$error" == *'Set VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID_PRODUCTION repository variable.'* ]]
