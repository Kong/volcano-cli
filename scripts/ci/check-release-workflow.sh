#!/usr/bin/env bash
set -euo pipefail

workflow=".github/workflows/release-please.yml"
check_workflow=".github/workflows/check.yml"
grep -Fxq -- '  group: release-please' "$workflow" || {
  echo "release workflow must serialize all refs in one concurrency group" >&2
  exit 1
}

for required in \
  "    if: \${{ github.ref == 'refs/heads/main' }}" \
  "gh pr list --state open --label 'autorelease: pending'" \
  'checks: read' \
  'pull-requests: read' \
  "CHECKS_TOKEN: \${{ github.token }}" \
  "head_oid=\"\$(jq -r '.headRefOid' <<< \"\$metadata\")\"" \
  'for attempt in {1..30}' \
  "checks_probe=\"\$(GH_TOKEN=\"\$CHECKS_TOKEN\" gh pr checks \"\$number\" --required --json name 2>&1)\"" \
  "if [ \"\$checks_status\" -eq 0 ] || [ \"\$checks_status\" -eq 8 ]; then" \
  'no (required )?checks reported' \
  'map(.name) | index("check / check") != null' \
  "GH_TOKEN=\"\$CHECKS_TOKEN\" gh pr checks \"\$number\" --required --watch --fail-fast" \
  "gh pr merge \"\$number\" --admin --squash --match-head-commit \"\$head_oid\""; do
  grep -Fq -- "$required" "$workflow" || {
    echo "release workflow is missing: $required" >&2
    exit 1
  }
done

grep -Fq -- 'run: make release-workflow-check' "$check_workflow" || {
  echo "CI does not run the release workflow check" >&2
  exit 1
}

if grep -Fq 'branches/main/protection' "$workflow"; then
  echo "release workflow must not require administration access to read branch protection" >&2
  exit 1
fi

if grep -Eq 'gh[[:space:]]+pr[[:space:]]+merge[[:space:]].*--auto([[:space:]]|$)' "$workflow"; then
  echo "release workflow must use the app bypass instead of review-gated auto-merge" >&2
  exit 1
fi
