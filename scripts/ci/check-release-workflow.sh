#!/usr/bin/env bash
set -euo pipefail

workflow=".github/workflows/release-please.yml"
check_workflow=".github/workflows/check.yml"
expected_checks='["Analyze (actions)","Analyze (go)","Analyze (javascript-typescript)","Analyze (python)","Analyze (ruby)","CodeQL","check / check","check / localmode-e2e","license/cla"]'
grep -Fxq -- '  group: release-please' "$workflow" || {
  echo "release workflow must serialize all refs in one concurrency group" >&2
  exit 1
}

for required in \
  "    if: \${{ github.ref == 'refs/heads/main' }}" \
  "RELEASE_PRS: \${{ steps.release.outputs.prs }}" \
  "gh pr list --state open --label 'autorelease: pending'" \
  'type == "array"' \
  'error("invalid release PR output")' \
  'sort -nu' \
  'checks: read' \
  'pull-requests: read' \
  "CHECKS_TOKEN: \${{ github.token }}" \
  "head_oid=\"\$(jq -r '.headRefOid' <<< \"\$metadata\")\"" \
  'for attempt in {1..30}' \
  "commits/\${head_oid}/check-runs?per_page=100" \
  "commits/\${head_oid}/status" \
  "expected_checks='$expected_checks'" \
  "\$expected - . | length == 0" \
  "GH_TOKEN=\"\$CHECKS_TOKEN\" gh pr checks \"\$number\" --required --watch --fail-fast" \
  "GH_TOKEN=\"\$CHECKS_TOKEN\" gh pr checks \"\$number\" --watch --fail-fast" \
  'all(.[]; .bucket == "pass" or .bucket == "skipping")' \
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

release_pr_filter='if type == "array" and all(.[]; (.number | type) == "number" and .number > 0 and .number == (.number | floor)) then .[].number else error("invalid release PR output") end'
fresh_release_prs='[{"number":181}]'
release_pr_numbers="$({
  jq -r "$release_pr_filter" <<< "$fresh_release_prs"
  printf '%s\n' 179
} | sort -nu)"
if [ "$release_pr_numbers" != $'179\n181' ]; then
  echo "release PR discovery did not combine fresh and existing PRs" >&2
  exit 1
fi
for invalid_release_prs in '{"entry":{"number":181}}' '[{"number":0}]' '[{"number":"181"}]'; do
  if jq -r "$release_pr_filter" <<< "$invalid_release_prs" >/dev/null 2>&1; then
    echo "release PR discovery accepted malformed action output" >&2
    exit 1
  fi
done

required_checks='["Analyze (actions)","Analyze (go)","Analyze (javascript-typescript)","Analyze (python)","Analyze (ruby)","CodeQL","check / check","check / localmode-e2e","license/cla"]'
check_runs="$(jq -c 'map(select(. != "license/cla") | {name: .}) | {check_runs: .}' <<< "$required_checks")"
commit_statuses='{"statuses":[{"context":"license/cla"}]}'
registered_checks="$({
  jq -r '.check_runs[].name' <<< "$check_runs"
  jq -r '.statuses[].context' <<< "$commit_statuses"
} | jq -Rsc 'split("\n") | map(select(length > 0))')"
jq -e --argjson expected "$expected_checks" '$expected - . | length == 0' <<< "$registered_checks" >/dev/null
if jq -e --argjson expected "$expected_checks" '$expected - . | length == 0' <<< '["check / check"]' >/dev/null; then
  echo "release check policy accepted an incomplete required-check set" >&2
  exit 1
fi

check_success='length > 0 and all(.[]; .bucket == "pass" or .bucket == "skipping")'
jq -e "$check_success" <<< '[{"bucket":"pass"},{"bucket":"skipping"}]' >/dev/null
if jq -e "$check_success" <<< '[{"bucket":"pass"},{"bucket":"fail"}]' >/dev/null; then
  echo "release check policy accepted a failed non-required check" >&2
  exit 1
fi

if grep -Fq 'branches/main/protection' "$workflow"; then
  echo "release workflow must not require administration access to read branch protection" >&2
  exit 1
fi

if grep -Eq 'gh[[:space:]]+pr[[:space:]]+merge[[:space:]].*--auto([[:space:]]|$)' "$workflow"; then
  echo "release workflow must use the app bypass instead of review-gated auto-merge" >&2
  exit 1
fi
