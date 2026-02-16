#!/bin/bash
set -e

cd "$(dirname "$0")"

# Check easytier binaries exist
if [ ! -f easytier-bin/easytier-core.exe ]; then
  echo "ERROR: easytier-bin/easytier-core.exe not found"
  echo "Download Windows amd64 EasyTier from https://github.com/EasyTier/EasyTier/releases"
  echo "and place easytier-core.exe in easytier-bin/"
  exit 1
fi

if [ ! -f easytier-bin/easytier-core-darwin ]; then
  echo "ERROR: easytier-bin/easytier-core-darwin not found"
  echo "Download macOS arm64 EasyTier from https://github.com/EasyTier/EasyTier/releases"
  echo "and place the binary as easytier-bin/easytier-core-darwin"
  exit 1
fi

mkdir -p dist

echo "Building Windows amd64..."
GOOS=windows GOARCH=amd64 go build -o dist/telehand.exe .

echo "Building macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -o dist/telehand-mac .

echo ""
echo "Build complete:"
ls -lh dist/
