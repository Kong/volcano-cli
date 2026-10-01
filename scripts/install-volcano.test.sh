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
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output)
      output="$2"
      shift 2
      ;;
    https://*)
      url="$1"
      shift
      ;;
    *) shift ;;
  esac
done

printf '%s\n' "$url" >> "$FAKE_CURL_LOG"
case "$url" in
  */latest-version) printf '%s\n' "${FAKE_LATEST_VERSION:-v1.2.3}" > "$output"; exit 0 ;;
esac
cp "$FAKE_VOLCANO_BINARY" "$output"
EOF
chmod +x "$TMP_DIR/bin/curl"

cat > "$TMP_DIR/bin/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) printf '%s\n' "$FAKE_UNAME_S" ;;
  -m) printf '%s\n' "$FAKE_UNAME_M" ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$TMP_DIR/bin/uname"

cat > "$TMP_DIR/volcano" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_VOLCANO_LOG"
EOF
chmod +x "$TMP_DIR/volcano"

cat > "$TMP_DIR/bin/cosign" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" > "$FAKE_COSIGN_LOG"
EOF
chmod +x "$TMP_DIR/bin/cosign"

export FAKE_VOLCANO_BINARY="$TMP_DIR/volcano"
export FAKE_VOLCANO_LOG="$TMP_DIR/volcano.log"
export FAKE_CURL_LOG="$TMP_DIR/curl.log"
export FAKE_COSIGN_LOG="$TMP_DIR/cosign.log"
export FAKE_UNAME_S="Darwin"
export FAKE_UNAME_M="arm64"
export PATH="$TMP_DIR/bin:$PATH"
export VOLCANO_INSTALL_DIR="$TMP_DIR/install"
export VOLCANO_SKIP_SIGNATURE_VERIFICATION=1

assert_asset() {
  asset_os="$1"
  asset_arch="$2"
  asset_name="$3"
  asset_dir="$TMP_DIR/install-$asset_os-$asset_arch"

  FAKE_UNAME_S="$asset_os" FAKE_UNAME_M="$asset_arch" VOLCANO_INSTALL_DIR="$asset_dir" \
    sh "$ROOT/scripts/install-volcano.sh" >/dev/null
  grep -Fx "https://download.volcano.dev/builds/releases/download/v1.2.3/$asset_name" "$FAKE_CURL_LOG" >/dev/null
  case "$asset_name" in
    *.exe) test -x "$asset_dir/volcano.exe" ;;
    *) test -x "$asset_dir/volcano" ;;
  esac
}

assert_rejected() {
  rejected_os="$1"
  rejected_arch="$2"
  rejected_message="$3"

  if FAKE_UNAME_S="$rejected_os" FAKE_UNAME_M="$rejected_arch" \
    sh "$ROOT/scripts/install-volcano.sh" >"$TMP_DIR/platform-error.log" 2>&1; then
    echo "expected $rejected_os $rejected_arch to fail" >&2
    exit 1
  fi
  grep -F "$rejected_message" "$TMP_DIR/platform-error.log" >/dev/null
}

sh "$ROOT/scripts/install-volcano.sh" >/dev/null
test -x "$TMP_DIR/install/volcano"
test ! -e "$FAKE_VOLCANO_LOG"

assert_asset Linux x86_64 volcano-linux-amd64
assert_asset Linux amd64 volcano-linux-amd64
assert_asset Linux arm64 volcano-linux-arm64
assert_asset Linux aarch64 volcano-linux-arm64
assert_asset Darwin x86_64 volcano-macos-amd64
assert_asset Darwin arm64 volcano-macos-arm64
assert_asset MINGW64_NT-10.0 x86_64 volcano-windows-amd64.exe
assert_rejected MINGW64_NT-10.0 arm64 "unsupported platform: windows-arm64"
assert_rejected Plan9 x86_64 "unsupported operating system: plan9"
assert_rejected Linux riscv64 "unsupported architecture: riscv64"

VOLCANO_VERSION=v1.2.3 sh "$ROOT/scripts/install-volcano.sh" >/dev/null
if VOLCANO_VERSION=v01.2.3 sh "$ROOT/scripts/install-volcano.sh" >"$TMP_DIR/version-error.log" 2>&1; then
  echo "expected invalid version to fail" >&2
  exit 1
fi
grep -F "unsupported Volcano CLI version selector: v01.2.3" "$TMP_DIR/version-error.log" >/dev/null

if VOLCANO_VERSION="$(printf 'v1.2.3\ninvalid')" \
  sh "$ROOT/scripts/install-volcano.sh" >"$TMP_DIR/version-error.log" 2>&1; then
  echo "expected multiline version to fail" >&2
  exit 1
fi
grep -F "unsupported Volcano CLI version selector: v1.2.3" "$TMP_DIR/version-error.log" >/dev/null

unset VOLCANO_SKIP_SIGNATURE_VERIFICATION
sh "$ROOT/scripts/install-volcano.sh" >/dev/null
grep -F -- "--certificate-identity https://github.com/Kong/volcano-cli/.github/workflows/publish-cli.yml@refs/tags/v1.2.3" "$FAKE_COSIGN_LOG" >/dev/null
VOLCANO_VERSION=v1.2.3 sh "$ROOT/scripts/install-volcano.sh" >/dev/null
grep -F -- "--certificate-identity https://github.com/Kong/volcano-cli/.github/workflows/publish-cli.yml@refs/tags/v1.2.3" "$FAKE_COSIGN_LOG" >/dev/null
export VOLCANO_SKIP_SIGNATURE_VERIFICATION=1

cat > "$TMP_DIR/bin/volcano" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$TMP_DIR/bin/volcano"

setup_output="$(sh "$ROOT/scripts/install-volcano.sh" --setup)"
printf '%s\n' "$setup_output" | grep -F \
  "Warning: 'volcano' on your PATH resolves to $TMP_DIR/bin/volcano, not $TMP_DIR/install/volcano." >/dev/null
test "$(cat "$FAKE_VOLCANO_LOG")" = "setup"

ln -sf "$TMP_DIR/install/volcano" "$TMP_DIR/bin/volcano"
path_output="$(sh "$ROOT/scripts/install-volcano.sh")"
if printf '%s\n' "$path_output" | grep -F "Warning:" >/dev/null; then
  echo "expected PATH symlink to resolve to the installed binary" >&2
  exit 1
fi

if sh "$ROOT/scripts/install-volcano.sh" --unknown >"$TMP_DIR/error.log" 2>&1; then
  echo "expected unknown option to fail" >&2
  exit 1
fi
grep -F "unknown option: --unknown" "$TMP_DIR/error.log" >/dev/null

# Promotion metadata cannot inject a path or mix release assets.
if FAKE_LATEST_VERSION='../bad' sh "$ROOT/scripts/install-volcano.sh" >"$TMP_DIR/version-error.log" 2>&1; then
  echo "expected malformed latest version to fail" >&2
  exit 1
fi
grep -F 'invalid latest Volcano CLI version' "$TMP_DIR/version-error.log" >/dev/null
VOLCANO_DOWNLOAD_URL=https://mirror.example/releases sh "$ROOT/scripts/install-volcano.sh" >/dev/null
grep -Fx 'https://mirror.example/releases/download/v1.2.3/volcano-macos-arm64' "$FAKE_CURL_LOG" >/dev/null
VOLCANO_GITHUB_RELEASES_URL=https://legacy.example/releases sh "$ROOT/scripts/install-volcano.sh" >/dev/null
grep -Fx 'https://legacy.example/releases/latest/download/volcano-macos-arm64' "$FAKE_CURL_LOG" >/dev/null
