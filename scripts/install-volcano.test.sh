#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/install"

cat > "$TMP_DIR/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output)
      output="$2"
      shift 2
      ;;
    *) shift ;;
  esac
done

cp "$FAKE_VOLCANO_BINARY" "$output"
EOF
chmod +x "$TMP_DIR/bin/curl"

cat > "$TMP_DIR/volcano" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE_VOLCANO_LOG"
EOF
chmod +x "$TMP_DIR/volcano"

export FAKE_VOLCANO_BINARY="$TMP_DIR/volcano"
export FAKE_VOLCANO_LOG="$TMP_DIR/volcano.log"
export PATH="$TMP_DIR/bin:$PATH"
export VOLCANO_INSTALL_DIR="$TMP_DIR/install"
export VOLCANO_SKIP_SIGNATURE_VERIFICATION=1

bash "$ROOT/scripts/install-volcano.sh" >/dev/null
test -x "$TMP_DIR/install/volcano"
test ! -e "$FAKE_VOLCANO_LOG"

bash "$ROOT/scripts/install-volcano.sh" --setup >/dev/null
test "$(cat "$FAKE_VOLCANO_LOG")" = "setup"

if bash "$ROOT/scripts/install-volcano.sh" --unknown >"$TMP_DIR/error.log" 2>&1; then
  echo "expected unknown option to fail" >&2
  exit 1
fi
grep -F "unknown option: --unknown" "$TMP_DIR/error.log" >/dev/null
