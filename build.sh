#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

./scripts/fetch-easytier.sh

mkdir -p dist

echo "Building Windows amd64..."
GOOS=windows GOARCH=amd64 go build -o dist/telehand.exe .

echo "Building macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -o dist/telehand-mac .

echo ""
echo "Build complete:"
ls -lh dist/
