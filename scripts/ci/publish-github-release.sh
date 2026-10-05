#!/usr/bin/env bash
# Publishes the GitHub release for a tag with every file in an asset directory.
# Release Please usually publishes the release before this runs, so an existing
# release gets the assets; a missing one is created with them.
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: ${0##*/} <tag> <title> <asset dir>" >&2
  exit 2
fi

tag="$1"
title="$2"
assets="$3"
max=4
delay="${RELEASE_RETRY_DELAY_SECONDS:-5}"

# Retry a command on transient failures (e.g. a GitHub API 5xx) with linear
# backoff; a persistent failure still fails the job loudly.
retry() {
  local n=0
  until "$@"; do
    n=$((n + 1))
    if [ "$n" -ge "$max" ]; then
      echo "giving up after ${max} attempts: $*" >&2
      return 1
    fi
    echo "attempt ${n} failed: $*; retrying in $((n * delay))s" >&2
    sleep $((n * delay))
  done
}

# Fails only when GitHub answers 404; any other error is retried, then fatal.
# `gh release view` would be ambiguous: it reports "release not found" when one
# of its two lookups 404s even if the other failed.
release_exists() {
  local n=0 err
  until err="$(gh api "repos/{owner}/{repo}/releases/tags/${tag}" --silent 2>&1)"; do
    if [[ "$err" == *"(HTTP 404)"* ]]; then
      return 1
    fi
    n=$((n + 1))
    if [ "$n" -ge "$max" ]; then
      echo "giving up checking for release ${tag} after ${max} attempts: ${err}" >&2
      exit 1
    fi
    echo "checking for release ${tag} failed (attempt ${n}): ${err}; retrying in $((n * delay))s" >&2
    sleep $((n * delay))
  done
}

n=0
until release_exists; do
  # The bundled create drafts the release, uploads the assets, then publishes
  # it, so a new release is never visible without its assets. After a failed
  # create, check again: the release may exist now.
  if gh release create "$tag" "$assets"/* \
    --target "$GITHUB_SHA" \
    --title "$title" \
    --generate-notes; then
    exit 0
  fi
  n=$((n + 1))
  if [ "$n" -ge "$max" ]; then
    echo "giving up creating release ${tag} after ${max} attempts" >&2
    exit 1
  fi
  echo "release create for ${tag} failed (attempt ${n}); retrying in $((n * delay))s" >&2
  sleep $((n * delay))
done

# Never delete an existing release here: Release Please may own it and its tag.
retry gh release upload "$tag" "$assets"/* --clobber
retry gh release edit "$tag" --title "$title" --target "$GITHUB_SHA"
