#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: ${0##*/} <pull command>..." >&2
  exit 2
fi

for attempt in {1..4}; do
  if "$@"; then
    exit 0
  fi
  if [ "$attempt" -eq 4 ]; then
    echo "image pull failed after $attempt attempts: $*" >&2
    exit 1
  fi
  sleep "${IMAGE_PULL_RETRY_DELAY_SECONDS:-15}"
done
