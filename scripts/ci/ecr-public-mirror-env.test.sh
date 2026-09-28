#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

digest="sha256:$(printf 'a%.0s' {1..64})"
mirrored="ghcr.io/kong/volcano-hosting-ecr-public/aws-appconfig/aws-appconfig-agent@$digest"

mkdir "$work/bin"
cat > "$work/bin/docker" <<'EOF'
#!/usr/bin/env bash
[ "$MIRROR_SERVES" = 1 ] && [ "${*: -1}" = "$MIRRORED" ] || exit 1
echo "${MIRRORED#*@}"
EOF
chmod +x "$work/bin/docker"

cat > "$work/compose.yml" <<EOF
services:
  # image: \${IGNORED_IMAGE:-public.ecr.aws/commented/out:1@$digest}
  appconfig-agent:
    image: \${VOLCANO_APPCONFIG_AGENT_IMAGE:-public.ecr.aws/aws-appconfig/aws-appconfig-agent:2.x@$digest}
  unpinned:
    image: \${UNPINNED_IMAGE:-public.ecr.aws/example/unpinned:latest}
  server:
    image: \${VOLCANO_IMAGE:-kong/volcano:local-nightly}
EOF

run() {
  : > "$work/env"
  PATH="$work/bin:$PATH" MIRROR_SERVES="$1" MIRRORED="$mirrored" \
    COMPOSE_TEMPLATE="$work/compose.yml" GITHUB_ENV="$work/env" \
    bash "$root/scripts/ci/ecr-public-mirror-env.sh" > "$work/out"
}

run 1
[ "$(cat "$work/env")" = "VOLCANO_APPCONFIG_AGENT_IMAGE=$mirrored" ]
grep -q 'ECR Public: public.ecr.aws/example/unpinned:latest$' "$work/out"
if grep -q 'commented' "$work/out"; then
  echo 'read a commented-out image' >&2
  exit 1
fi

run 0
[ ! -s "$work/env" ]
grep -q 'aws-appconfig-agent:2.x@' "$work/out"

# The shipped template must keep the AppConfig agent redirectable.
grep -qF 'VOLCANO_APPCONFIG_AGENT_IMAGE:-public.ecr.aws/' \
  "$root/internal/localmode/assets/docker-compose.template.yml"
