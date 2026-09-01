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
  'for attempt in {1..50}' \
  "commits/\${head_oid}/check-runs?filter=latest&per_page=100" \
  "commits/\${head_oid}/status" \
  "expected_checks='$expected_checks'" \
  'any(.[]; .state == "fail")' \
  '(length > 0 and all(.[]; .state == "pass"))' \
  '.conclusion == "success" or .conclusion == "neutral" or .conclusion == "skipped"' \
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
# jq variables expand inside jq, not the shell.
# shellcheck disable=SC2016
check_result_filter='[
  $runs.check_runs[] | {
    name,
    state: if .status != "completed" then "pending"
      elif .conclusion == "success" or .conclusion == "neutral" or .conclusion == "skipped" then "pass"
      else "fail"
      end
  }
] + [
  ($statuses.statuses | group_by(.context)[] | max_by(.updated_at)) | {
    name: .context,
    state: if .state == "success" then "pass"
      elif .state == "pending" then "pending"
      else "fail"
      end
  }
]'
check_runs="$(jq -c 'map(select(. != "license/cla") | {name: ., status: "completed", conclusion: "success"}) | {check_runs: .}' <<< "$required_checks")"
commit_statuses='{"statuses":[{"context":"license/cla","state":"failure","updated_at":"2026-01-01T00:00:00Z"},{"context":"license/cla","state":"success","updated_at":"2026-01-01T00:01:00Z"}]}'
check_results="$(jq -n --argjson runs "$check_runs" --argjson statuses "$commit_statuses" "$check_result_filter")"
# jq variables expand inside jq, not the shell.
# shellcheck disable=SC2016
check_success='($expected - [.[].name] | length == 0) and (length > 0 and all(.[]; .state == "pass"))'
jq -e --argjson expected "$expected_checks" "$check_success" <<< "$check_results" >/dev/null
if jq -e 'any(.[]; .state == "fail")' <<< "$check_results" >/dev/null; then
  echo "release check policy used an old status instead of the latest context status" >&2
  exit 1
fi
pending_results="$(jq 'map(if .name == "check / check" then .state = "pending" else . end)' <<< "$check_results")"
if jq -e --argjson expected "$expected_checks" "$check_success" <<< "$pending_results" >/dev/null; then
  echo "release check policy accepted a pending check" >&2
  exit 1
fi
failed_results="$(jq 'map(if .name == "check / check" then .state = "fail" else . end)' <<< "$check_results")"
if ! jq -e 'any(.[]; .state == "fail")' <<< "$failed_results" >/dev/null; then
  echo "release check policy did not reject a failed check" >&2
  exit 1
fi
incomplete_results="$(jq 'map(select(.name != "CodeQL"))' <<< "$check_results")"
if jq -e --argjson expected "$expected_checks" "$check_success" <<< "$incomplete_results" >/dev/null; then
  echo "release check policy accepted an incomplete required-check set" >&2
  exit 1
fi

if grep -Fq 'branches/main/protection' "$workflow"; then
  echo "release workflow must not require administration access to read branch protection" >&2
  exit 1
fi

if grep -Fq 'gh pr checks' "$workflow"; then
  echo "release workflow must validate checks against the captured commit" >&2
  exit 1
fi

if grep -Eq 'gh[[:space:]]+pr[[:space:]]+merge[[:space:]].*--auto([[:space:]]|$)' "$workflow"; then
  echo "release workflow must use the app bypass instead of review-gated auto-merge" >&2
  exit 1
fi
