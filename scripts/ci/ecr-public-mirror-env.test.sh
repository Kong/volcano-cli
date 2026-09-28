#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

digest="sha256:$(printf 'a%.0s' {1..64})"
mirrored="ghcr.io/kong/volcano-hosting-ecr-public/aws-appconfig/aws-appconfig-agent@$digest"

mkdir "$work/bin"
# The stub mirror answers any lookup, so only the script's own checks decide
# which images it redirects.
cat > "$work/bin/docker" <<'EOF'
#!/usr/bin/env bash
ref="${*: -1}"
case "$MIRROR" in
  serves) echo "${ref#*@}" ;;
  drifted) echo "sha256:$(printf 'b%.0s' {1..64})" ;;
  *) exit 1 ;;
esac
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
  PATH="$work/bin:$PATH" MIRROR="$1" \
    COMPOSE_TEMPLATE="$work/compose.yml" GITHUB_ENV="$work/env" \
    bash "$root/scripts/ci/ecr-public-mirror-env.sh" > "$work/out"
}

run serves
[ "$(cat "$work/env")" = "VOLCANO_APPCONFIG_AGENT_IMAGE=$mirrored" ]
grep -q 'ECR Public: public.ecr.aws/example/unpinned:latest$' "$work/out"

run drifted
[ ! -s "$work/env" ]

run unavailable
[ ! -s "$work/env" ]
grep -q 'aws-appconfig-agent:2.x@' "$work/out"

# The shipped template must keep the AppConfig agent redirectable.
grep -qF 'VOLCANO_APPCONFIG_AGENT_IMAGE:-public.ecr.aws/' \
  "$root/internal/localmode/assets/docker-compose.template.yml"
