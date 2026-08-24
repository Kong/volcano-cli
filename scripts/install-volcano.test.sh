#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/install"

cat > "$TMP_DIR/bin/curl" <<'EOF'
#!/bin/sh
set -eu

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
#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_VOLCANO_LOG"
EOF
chmod +x "$TMP_DIR/volcano"

export FAKE_VOLCANO_BINARY="$TMP_DIR/volcano"
export FAKE_VOLCANO_LOG="$TMP_DIR/volcano.log"
export PATH="$TMP_DIR/bin:$PATH"
export VOLCANO_INSTALL_DIR="$TMP_DIR/install"
export VOLCANO_SKIP_SIGNATURE_VERIFICATION=1

sh "$ROOT/scripts/install-volcano.sh" >/dev/null
test -x "$TMP_DIR/install/volcano"
test ! -e "$FAKE_VOLCANO_LOG"

VOLCANO_VERSION=v1.2.3 sh "$ROOT/scripts/install-volcano.sh" >/dev/null
if VOLCANO_VERSION=v01.2.3 sh "$ROOT/scripts/install-volcano.sh" >"$TMP_DIR/version-error.log" 2>&1; then
  echo "expected invalid version to fail" >&2
  exit 1
fi
grep -F "unsupported Volcano CLI version selector: v01.2.3" "$TMP_DIR/version-error.log" >/dev/null

sh "$ROOT/scripts/install-volcano.sh" --setup >/dev/null
test "$(cat "$FAKE_VOLCANO_LOG")" = "setup"

if sh "$ROOT/scripts/install-volcano.sh" --unknown >"$TMP_DIR/error.log" 2>&1; then
  echo "expected unknown option to fail" >&2
  exit 1
fi
grep -F "unknown option: --unknown" "$TMP_DIR/error.log" >/dev/null
