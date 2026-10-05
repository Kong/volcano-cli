#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir "$work/bin" "$work/assets"
touch "$work/assets/install.sh" "$work/assets/volcano-linux-amd64"

# Logs every call and answers release lookups and creates from scripted
# outcomes, one per call; the last outcome repeats.
cat > "$work/bin/gh" <<'EOF'
#!/usr/bin/env bash
echo "$*" >> "$GH_CALLS"
case "$1 $2" in
  api\ *) outcomes="$GH_LOOKUPS" ;;
  "release create") outcomes="$GH_CREATES" ;;
  *) exit 0 ;;
esac
outcome="$(head -n 1 "$outcomes")"
if [ "$(wc -l < "$outcomes")" -gt 1 ]; then
  tail -n +2 "$outcomes" > "$outcomes.next"
  mv "$outcomes.next" "$outcomes"
fi
case "$outcome" in
  ok) ;;
  missing) echo 'gh: Not Found (HTTP 404)' >&2; exit 1 ;;
  offline) echo 'dial tcp [::1]:443: connect: connection refused' >&2; exit 1 ;;
  exists) echo 'a release with the same tag name already exists: v1.2.3' >&2; exit 1 ;;
esac
EOF
chmod +x "$work/bin/gh"

# run <lookup outcomes> <create outcomes>
run() {
  : > "$work/calls"
  tr ' ' '\n' <<< "$1" > "$work/lookups"
  tr ' ' '\n' <<< "$2" > "$work/creates"
  PATH="$work/bin:$PATH" GH_CALLS="$work/calls" \
    GH_LOOKUPS="$work/lookups" GH_CREATES="$work/creates" \
    GITHUB_SHA=abc123 RELEASE_RETRY_DELAY_SECONDS=0 \
    bash "$root/scripts/ci/publish-github-release.sh" v1.2.3 v1.2.3 "$work/assets"
}

expect_calls() {
  local want
  want="$(printf '%s\n' "$@")"
  if [ "$(cat "$work/calls")" != "$want" ]; then
    printf 'gh calls:\n%s\nwant:\n%s\n' "$(cat "$work/calls")" "$want" >&2
    exit 1
  fi
}

assets="$work/assets/install.sh $work/assets/volcano-linux-amd64"
lookup='api repos/{owner}/{repo}/releases/tags/v1.2.3 --silent'
create="release create v1.2.3 $assets --target abc123 --title v1.2.3 --generate-notes"
upload="release upload v1.2.3 $assets --clobber"
edit='release edit v1.2.3 --title v1.2.3 --target abc123'

# Release Please already published the release.
run ok ok
expect_calls "$lookup" "$upload" "$edit"

# A lookup error is retried, never treated as a missing release.
run 'offline ok' ok
expect_calls "$lookup" "$lookup" "$upload" "$edit"

# The release appeared after the lookup, so create refuses and the assets
# still go to the existing release.
run 'missing ok' exists
expect_calls "$lookup" "$create" "$lookup" "$upload" "$edit"

run missing ok
expect_calls "$lookup" "$create"

# A failed create is retried without deleting anything in between.
run missing 'offline ok'
expect_calls "$lookup" "$create" "$lookup" "$create"

if run offline ok; then
  echo 'published a release whose existence could not be checked' >&2
  exit 1
fi
expect_calls "$lookup" "$lookup" "$lookup" "$lookup"
