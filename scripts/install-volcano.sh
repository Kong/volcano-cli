#!/bin/sh
set -eu

readonly VOLCANO_GITHUB_RELEASES_URL="${VOLCANO_GITHUB_RELEASES_URL:-https://github.com/Kong/volcano-cli/releases}"
readonly VOLCANO_DEFAULT_VERSION="latest"
readonly VOLCANO_SIGNATURE_WORKFLOW="https://github.com/Kong/volcano-cli/.github/workflows/publish-cli.yml"
readonly VOLCANO_SIGNATURE_OIDC_ISSUER="https://token.actions.githubusercontent.com"
readonly VOLCANO_STABLE_TAG_SIGNATURE_IDENTITY_RE="^https://github[.]com/Kong/volcano-cli/[.]github/workflows/publish-cli[.]yml@refs/tags/v(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)$"
VOLCANO_INSTALL_DIR="${VOLCANO_INSTALL_DIR:-}"
RUN_SETUP=0

fail() {
  echo "Error: $*" >&2
  exit 1
}

have() {
  command -v "$1" >/dev/null 2>&1
}

is_semver() {
  printf '%s\n' "$1" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
}

detect_os() {
  detect_os_raw="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$detect_os_raw" in
    linux*) echo "linux" ;;
    darwin*) echo "macos" ;;
    mingw* | msys* | cygwin*) echo "windows" ;;
    *) fail "unsupported operating system: ${detect_os_raw}" ;;
  esac
}

detect_arch() {
  detect_arch_raw="$(uname -m)"
  case "$detect_arch_raw" in
    x86_64 | amd64) echo "amd64" ;;
    arm64 | aarch64) echo "arm64" ;;
    *) fail "unsupported architecture: ${detect_arch_raw}" ;;
  esac
}

download_file() {
  download_file_url="$1"
  download_file_output="$2"
  if have curl; then
    curl --fail --location --silent --show-error "$download_file_url" --output "$download_file_output"
    return
  fi
  if have wget; then
    wget --quiet --output-document="$download_file_output" "$download_file_url"
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
  verify_signature_file="$1"
  verify_signature_bundle="$2"
  verify_signature_version="$3"

  if [ "${VOLCANO_SKIP_SIGNATURE_VERIFICATION:-}" = "1" ]; then
    echo "Skipping Volcano CLI signature verification because VOLCANO_SKIP_SIGNATURE_VERIFICATION=1."
    return
  fi

  case "$verify_signature_version" in
    latest)
      cosign verify-blob "$verify_signature_file" \
        --bundle "$verify_signature_bundle" \
        --certificate-identity-regexp "$VOLCANO_STABLE_TAG_SIGNATURE_IDENTITY_RE" \
        --certificate-oidc-issuer "$VOLCANO_SIGNATURE_OIDC_ISSUER"
      ;;
    *)
      if is_semver "$verify_signature_version"; then
        verify_signature_identity="${VOLCANO_SIGNATURE_WORKFLOW}@refs/tags/${verify_signature_version}"
        cosign verify-blob "$verify_signature_file" \
          --bundle "$verify_signature_bundle" \
          --certificate-identity "$verify_signature_identity" \
          --certificate-oidc-issuer "$VOLCANO_SIGNATURE_OIDC_ISSUER"
      else
        fail "cannot verify signature for unsupported Volcano CLI version selector: ${verify_signature_version}; use latest or vMAJOR.MINOR.PATCH"
      fi
      ;;
  esac

  echo "Verified Volcano CLI signature for ${verify_signature_version}."
}

release_asset_url() {
  release_asset_url_version="$1"
  release_asset_url_asset="$2"

  case "$release_asset_url_version" in
    latest)
      echo "${VOLCANO_GITHUB_RELEASES_URL%/}/latest/download/${release_asset_url_asset}"
      ;;
    *)
      if is_semver "$release_asset_url_version"; then
        echo "${VOLCANO_GITHUB_RELEASES_URL%/}/download/${release_asset_url_version}/${release_asset_url_asset}"
      else
        fail "unsupported Volcano CLI version selector: ${release_asset_url_version}; use latest or vMAJOR.MINOR.PATCH"
      fi
      ;;
  esac
}

resolve_install_dir() {
  resolve_install_dir_os="$1"

  if [ -n "$VOLCANO_INSTALL_DIR" ]; then
    echo "$VOLCANO_INSTALL_DIR"
    return
  fi

  if [ "$resolve_install_dir_os" = "windows" ]; then
    echo "${HOME}/bin"
    return
  fi

  if [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
    echo "/usr/local/bin"
    return
  fi

  echo "${HOME}/.local/bin"
}

for arg in "$@"; do
  case "$arg" in
    --setup) RUN_SETUP=1 ;;
    *) fail "unknown option: ${arg}" ;;
  esac
done

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

# Record the install method so `volcano upgrade` re-runs this installer path
# (self-replace) rather than mistaking it for a package-manager install.
printf 'script\n' > "${INSTALL_DIR}/.volcano-install-method" 2>/dev/null || true

echo "Installed Volcano CLI to ${INSTALL_PATH}"
PATH_VOLCANO="$(command -v "$CLI_COMMAND" 2>/dev/null || true)"
if [ -z "$PATH_VOLCANO" ]; then
  echo "Add ${INSTALL_DIR} to your PATH to run '${CLI_COMMAND}' from any shell."
  echo "Run: ${INSTALL_PATH} --help"
elif [ "$PATH_VOLCANO" != "$INSTALL_PATH" ]; then
  echo "Warning: '${CLI_COMMAND}' on your PATH resolves to ${PATH_VOLCANO}, not ${INSTALL_PATH}."
  echo "Move ${INSTALL_DIR} earlier in your PATH or run '${INSTALL_PATH}' directly."
  echo "Run: ${INSTALL_PATH} --help"
elif [ "$RUN_SETUP" != "1" ]; then
  echo "Run: ${CLI_COMMAND} --help"
fi

if [ "$RUN_SETUP" = "1" ]; then
  "$INSTALL_PATH" setup
fi
