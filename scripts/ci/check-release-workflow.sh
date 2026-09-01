#!/usr/bin/env bash
set -euo pipefail

workflow=".github/workflows/release-please.yml"
for required in \
  "gh pr list --state open --label 'autorelease: pending'" \
  'required_status_checks' \
  "head_oid=\"\$(jq -r '.headRefOid' <<< \"\$metadata\")\"" \
  'for attempt in {1..30}' \
  'no (required )?checks reported' \
  "gh pr checks \"\$number\" --required --watch --fail-fast" \
  "gh pr merge \"\$number\" --admin --squash --match-head-commit \"\$head_oid\""; do
  grep -Fq -- "$required" "$workflow" || {
    echo "release workflow is missing: $required" >&2
    exit 1
  }
done

if grep -Eq 'gh[[:space:]]+pr[[:space:]]+merge[[:space:]].*--auto([[:space:]]|$)' "$workflow"; then
  echo "release workflow must use the app bypass instead of review-gated auto-merge" >&2
  exit 1
fi
