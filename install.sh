#!/bin/bash
set -e

REPO="sfpprxy/telehand"
BINARY_NAME="telehand"
VERSION=""

usage() {
  echo "Usage: $0 [--version <vX.Y.Z[-alpha.N]>]"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      if [[ -z "${2:-}" ]]; then
        echo "Error: --version requires a value"
        usage
        exit 1
      fi
      VERSION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Error: unknown argument '$1'"
      usage
      exit 1
      ;;
  esac
done

VERSION_SOURCE="latest release"
if [[ -n "$VERSION" ]]; then
  if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$ ]]; then
    echo "Error: invalid --version '$VERSION'. Version must include a 'v' prefix, e.g. v0.2.0-alpha.1"
    exit 1
  fi
  VERSION_SOURCE="specified tag"
fi

# Detect OS and arch
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  darwin) GOOS="darwin" ;;
  linux)  GOOS="linux" ;;
  *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64)  GOARCH="amd64" ;;
  arm64|aarch64) GOARCH="arm64" ;;
  *)             echo "Unsupported arch: $ARCH"; exit 1 ;;
esac

# Get latest version with fallback
get_latest_version() {
  for api_url in \
    "https://api.github.com/repos/${REPO}/releases/latest" \
    "https://ghfast.top/https://api.github.com/repos/${REPO}/releases/latest"; do
    ver=$(curl -fsSL "$api_url" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -n "$ver" ]; then echo "$ver"; return; fi
  done
}

if [[ -z "$VERSION" ]]; then
  VERSION=$(get_latest_version)
  if [ -z "$VERSION" ]; then
    echo "Failed to get latest version"
    exit 1
  fi
fi

FILENAME="${BINARY_NAME}-${GOOS}-${GOARCH}-${VERSION}.zip"

# Download sources with fallback
URLS=(
  "https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
  "https://ghfast.top/https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
)

echo "Installing ${BINARY_NAME} ${VERSION} (${GOOS}/${GOARCH}, source: ${VERSION_SOURCE})..."

DOWNLOADED=false
for url in "${URLS[@]}"; do
  echo "Trying: ${url}"
  if curl -fsSL "$url" -o "${FILENAME}" 2>/dev/null; then
    DOWNLOADED=true
    break
  fi
  echo "  Failed, trying next source..."
done

if [ "$DOWNLOADED" = false ]; then
  echo "Error: Failed to download from all sources"
  exit 1
fi

unzip -o "${FILENAME}" -d .
rm -f "${FILENAME}"
chmod +x "${BINARY_NAME}"

echo ""
echo "Installed ${BINARY_NAME} ${VERSION} to $(pwd)/${BINARY_NAME}"
echo "Run './${BINARY_NAME}' to start."
