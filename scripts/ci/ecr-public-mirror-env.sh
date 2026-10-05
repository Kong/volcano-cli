#!/usr/bin/env bash
# Anonymous public.ecr.aws pulls share a data quota with every workload behind
# the runner's egress IP, so a throttled runner can fail a release.
# Kong/volcano-hosting's ecr-public-mirror.yml keeps digest-pinned copies of the
# local-mode images in GHCR; this points each ${VARIABLE:-public.ecr.aws/...}
# compose image at its copy through $GITHUB_ENV. An image the mirror lacks or
# cannot serve keeps its ECR Public source.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
compose_template="${COMPOSE_TEMPLATE:-$root/internal/localmode/assets/docker-compose.template.yml}"
source_registry="public.ecr.aws"
mirror="ghcr.io/kong/volcano-hosting-ecr-public"

# "VARIABLE reference" pairs from ${VARIABLE:-public.ecr.aws/...} defaults.
overrides() {
  { grep -v '^[[:space:]]*#' "$compose_template" || true; } \
    | sed -nE 's/.*\$\{([A-Z0-9_]+):-(public\.ecr\.aws\/[^}]+)\}.*/\1 \2/p' \
    | LC_ALL=C sort -u
}

# Prints the mirrored copy of a digest-pinned reference, or fails when the
# reference has no digest or the mirror cannot serve that exact digest.
mirrored() {
  local ref="$1" name digest repository target found
  [[ "$ref" == *@sha256:* ]] || return 1
  name="${ref%@*}"
  digest="${ref#*@}"
  repository="$name"
  if [[ "${name##*/}" == *:* ]]; then
    repository="${name%:*}"
  fi
  target="$mirror/${repository#"$source_registry/"}"
  found="$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$target@$digest")" || return 1
  [ "$found" = "$digest" ] || return 1
  printf '%s@%s\n' "$target" "$digest"
}

missing=""
while read -r variable ref; do
  [ -n "$variable" ] || continue
  if pinned="$(mirrored "$ref")"; then
    echo "Compose resolves $ref from $pinned"
    echo "$variable=$pinned" >> "$GITHUB_ENV"
  else
    missing="$missing $ref"
  fi
done < <(overrides)

if [ -n "$missing" ]; then
  echo "::warning::The GHCR mirror could not supply these images, so they pull anonymously from ECR Public:$missing"
fi
