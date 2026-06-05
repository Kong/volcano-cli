#!/usr/bin/env bash
set -euo pipefail

readonly VOLCANO_GITHUB_RELEASES_URL="${VOLCANO_GITHUB_RELEASES_URL:-https://github.com/Kong/volcano-cli/releases}"
readonly VOLCANO_DEFAULT_VERSION="latest"
readonly VOLCANO_SIGNATURE_WORKFLOW="https://github.com/Kong/volcano-cli/.github/workflows/publish-cli.yml"
readonly VOLCANO_SIGNATURE_OIDC_ISSUER="https://token.actions.githubusercontent.com"
readonly VOLCANO_STABLE_TAG_SIGNATURE_IDENTITY_RE="^https://github[.]com/Kong/volcano-cli/[.]github/workflows/publish-cli[.]yml@refs/tags/v(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)$"
VOLCANO_INSTALL_DIR="${VOLCANO_INSTALL_DIR:-}"

fail() {
  echo "Error: $*" >&2
  exit 1
}

have() {
  command -v "$1" >/dev/null 2>&1
}

detect_os() {
  local raw
  raw="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$raw" in
    linux*) echo "linux" ;;
    darwin*) echo "macos" ;;
    mingw* | msys* | cygwin*) echo "windows" ;;
    *) fail "unsupported operating system: ${raw}" ;;
  esac
}

detect_arch() {
  local raw
  raw="$(uname -m)"
  case "$raw" in
    x86_64 | amd64) echo "amd64" ;;
    arm64 | aarch64) echo "arm64" ;;
    *) fail "unsupported architecture: ${raw}" ;;
  esac
}

download_file() {
  local url="$1"
  local output="$2"
  if have curl; then
    curl --fail --location --silent --show-error "$url" --output "$output"
    return
  fi
  if have wget; then
    wget --quiet --output-document="$output" "$url"
    return
  fi
  fail "curl or wget is required to download Volcano CLI"
}

require_cosign_for_verification() {
  if [ "${VOLCANO_SKIP_SIGNATURE_VERIFICATION:-}" = "1" ]; then
    return
  fi
  if ! have cosign; then
    fail "cosign is required to verify Volcano CLI signatures. Install cosign, or set VOLCANO_SKIP_SIGNATURE_VERIFICATION=1 to install without verification."
  fi
}

verify_signature() {
  local file="$1"
  local bundle="$2"
  local version="$3"
  local semver_re
  local identity

  if [ "${VOLCANO_SKIP_SIGNATURE_VERIFICATION:-}" = "1" ]; then
    echo "Skipping Volcano CLI signature verification because VOLCANO_SKIP_SIGNATURE_VERIFICATION=1."
    return
  fi

  semver_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
  case "$version" in
    latest)
      cosign verify-blob "$file" \
        --bundle "$bundle" \
        --certificate-identity-regexp "$VOLCANO_STABLE_TAG_SIGNATURE_IDENTITY_RE" \
        --certificate-oidc-issuer "$VOLCANO_SIGNATURE_OIDC_ISSUER"
      ;;
    *)
      if [[ "$version" =~ $semver_re ]]; then
        identity="${VOLCANO_SIGNATURE_WORKFLOW}@refs/tags/${version}"
        cosign verify-blob "$file" \
          --bundle "$bundle" \
          --certificate-identity "$identity" \
          --certificate-oidc-issuer "$VOLCANO_SIGNATURE_OIDC_ISSUER"
      else
        fail "cannot verify signature for unsupported Volcano CLI version selector: ${version}; use latest or vMAJOR.MINOR.PATCH"
      fi
      ;;
  esac

  echo "Verified Volcano CLI signature for ${version}."
}

release_asset_url() {
  local version="$1"
  local asset="$2"
  local semver_re

  semver_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
  case "$version" in
    latest)
      echo "${VOLCANO_GITHUB_RELEASES_URL%/}/latest/download/${asset}"
      ;;
    *)
      if [[ "$version" =~ $semver_re ]]; then
        echo "${VOLCANO_GITHUB_RELEASES_URL%/}/download/${version}/${asset}"
      else
        fail "unsupported Volcano CLI version selector: ${version}; use latest or vMAJOR.MINOR.PATCH"
      fi
      ;;
  esac
}

resolve_install_dir() {
  local os="$1"

  if [ -n "$VOLCANO_INSTALL_DIR" ]; then
    echo "$VOLCANO_INSTALL_DIR"
    return
  fi

  if [ "$os" = "windows" ]; then
    echo "${HOME}/bin"
    return
  fi

  if [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
    echo "/usr/local/bin"
    return
  fi

  echo "${HOME}/.local/bin"
}

VERSION="${VOLCANO_VERSION:-$VOLCANO_DEFAULT_VERSION}"

if [ -z "$VERSION" ]; then
  fail "VERSION is empty"
fi

OS="$(detect_os)"
ARCH="$(detect_arch)"
EXT=""
if [ "$OS" = "windows" ]; then
  EXT=".exe"
fi

TARGET="${OS}-${ARCH}"
case "$TARGET" in
  linux-amd64 | linux-arm64 | macos-amd64 | macos-arm64 | windows-amd64) ;;
  windows-arm64) fail "unsupported platform: ${TARGET}; Volcano CLI does not publish a Windows arm64 binary yet" ;;
  *) fail "unsupported platform: ${TARGET}" ;;
esac

BINARY_NAME="volcano-${TARGET}${EXT}"
DOWNLOAD_URL="$(release_asset_url "$VERSION" "$BINARY_NAME")"
BUNDLE_URL="${DOWNLOAD_URL}.sigstore.json"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
TMP_FILE="${TMP_DIR}/${BINARY_NAME}"
TMP_BUNDLE="${TMP_FILE}.sigstore.json"

require_cosign_for_verification

echo "Downloading Volcano CLI (${TARGET}) from ${DOWNLOAD_URL}"
download_file "$DOWNLOAD_URL" "$TMP_FILE"
if [ "${VOLCANO_SKIP_SIGNATURE_VERIFICATION:-}" != "1" ]; then
  download_file "$BUNDLE_URL" "$TMP_BUNDLE"
fi
verify_signature "$TMP_FILE" "$TMP_BUNDLE" "$VERSION"

INSTALL_DIR="$(resolve_install_dir "$OS")"
mkdir -p "$INSTALL_DIR"
if [ ! -w "$INSTALL_DIR" ]; then
  fail "install directory is not writable: ${INSTALL_DIR}. Set VOLCANO_INSTALL_DIR to a writable path."
fi

INSTALL_PATH="${INSTALL_DIR}/volcano${EXT}"
CLI_COMMAND="volcano${EXT}"
if have install && [ "$OS" != "windows" ]; then
  install -m 0755 "$TMP_FILE" "$INSTALL_PATH"
else
  cp "$TMP_FILE" "$INSTALL_PATH"
  chmod 0755 "$INSTALL_PATH" || true
fi

echo "Installed Volcano CLI to ${INSTALL_PATH}"
PATH_VOLCANO="$(command -v "$CLI_COMMAND" 2>/dev/null || true)"
if [ -z "$PATH_VOLCANO" ]; then
  echo "Add ${INSTALL_DIR} to your PATH to run '${CLI_COMMAND}' from any shell."
  echo "Run: ${INSTALL_PATH} --help"
elif [ "$PATH_VOLCANO" != "$INSTALL_PATH" ] && ! [ "$PATH_VOLCANO" -ef "$INSTALL_PATH" ]; then
  echo "Warning: '${CLI_COMMAND}' on your PATH resolves to ${PATH_VOLCANO}, not ${INSTALL_PATH}."
  echo "Move ${INSTALL_DIR} earlier in your PATH or run '${INSTALL_PATH}' directly."
  echo "Run: ${INSTALL_PATH} --help"
else
  echo "Run: ${CLI_COMMAND} --help"
fi
