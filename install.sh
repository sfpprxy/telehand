#!/bin/bash
set -e

REPO="sfpprxy/telehand"
BINARY_NAME="telehand"

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

# Get latest version
get_latest_version() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | \
    grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' || echo ""
}

VERSION=$(get_latest_version)
if [ -z "$VERSION" ]; then
  echo "Failed to get latest version"
  exit 1
fi

FILENAME="${BINARY_NAME}-${GOOS}-${GOARCH}-${VERSION}.zip"

# Download sources with fallback
URLS=(
  "https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
  "https://ghfast.top/https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
)

echo "Installing ${BINARY_NAME} ${VERSION} (${GOOS}/${GOARCH})..."

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
