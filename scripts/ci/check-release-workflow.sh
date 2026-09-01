#!/usr/bin/env bash
set -euo pipefail

workflow=".github/workflows/release-please.yml"
for required in \
  "gh pr list --state open --label 'autorelease: pending'" \
  'gh pr checks "$number" --required --watch --fail-fast' \
  'gh pr merge "$number" --admin --squash'; do
  grep -Fq -- "$required" "$workflow" || {
    echo "release workflow is missing: $required" >&2
    exit 1
  }
done

if grep -Fq -- 'gh pr merge "$number" --auto' "$workflow"; then
  echo "release workflow must use the app bypass instead of review-gated auto-merge" >&2
  exit 1
fi
