#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
state="$(mktemp)"
trap 'rm -f "$state"' EXIT

pull='n=$(cat "$STATE"); n=$((n + 1)); echo "$n" > "$STATE"; [ "$n" -ge "$SUCCEED_ON" ]'
echo 0 > "$state"
STATE="$state" SUCCEED_ON=3 IMAGE_PULL_RETRY_DELAY_SECONDS=0 \
  bash "$root/scripts/ci/retry-image-pull.sh" bash -c "$pull"
[ "$(cat "$state")" -eq 3 ]

echo 0 > "$state"
if STATE="$state" SUCCEED_ON=99 IMAGE_PULL_RETRY_DELAY_SECONDS=0 \
  bash "$root/scripts/ci/retry-image-pull.sh" bash -c "$pull"; then
  echo 'accepted failed pull' >&2
  exit 1
fi
[ "$(cat "$state")" -eq 4 ]
