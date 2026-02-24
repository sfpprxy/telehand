#!/bin/bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

ET_VERSION="${ET_VERSION:-2.4.5}"
BIN_DIR="easytier-bin"
WIN_ASSET="easytier-windows-x86_64-v${ET_VERSION}.zip"
MAC_ASSET="easytier-macos-aarch64-v${ET_VERSION}.zip"

mkdir -p "$BIN_DIR"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

download_asset() {
  local asset="$1"
  local out="$TMP_DIR/$asset"
  curl -fsSL "https://github.com/EasyTier/EasyTier/releases/download/v${ET_VERSION}/${asset}" -o "$out"
  echo "$out"
}

extract_file() {
  local zip_file="$1"
  local inside_path="$2"
  local dest_path="$3"
  unzip -o -j "$zip_file" "$inside_path" -d "$TMP_DIR/extract" >/dev/null
  cp "$TMP_DIR/extract/$(basename "$inside_path")" "$dest_path"
  rm -f "$TMP_DIR/extract/"*
}

need_windows=false
for f in easytier-core.exe easytier-cli.exe Packet.dll wintun.dll; do
  if [ ! -f "$BIN_DIR/$f" ]; then
    need_windows=true
  fi
done

if [ "$need_windows" = true ]; then
  echo "Downloading EasyTier Windows assets (v${ET_VERSION})..."
  win_zip="$(download_asset "$WIN_ASSET")"
  extract_file "$win_zip" "easytier-windows-x86_64/easytier-core.exe" "$BIN_DIR/easytier-core.exe"
  extract_file "$win_zip" "easytier-windows-x86_64/easytier-cli.exe" "$BIN_DIR/easytier-cli.exe"
  extract_file "$win_zip" "easytier-windows-x86_64/Packet.dll" "$BIN_DIR/Packet.dll"
  extract_file "$win_zip" "easytier-windows-x86_64/wintun.dll" "$BIN_DIR/wintun.dll"
  chmod +x "$BIN_DIR/easytier-core.exe" "$BIN_DIR/easytier-cli.exe"
fi

need_darwin=false
for f in easytier-core-darwin easytier-cli-darwin; do
  if [ ! -f "$BIN_DIR/$f" ]; then
    need_darwin=true
  fi
done

if [ "$need_darwin" = true ]; then
  echo "Downloading EasyTier macOS assets (v${ET_VERSION})..."
  mac_zip="$(download_asset "$MAC_ASSET")"
  extract_file "$mac_zip" "easytier-macos-aarch64/easytier-core" "$BIN_DIR/easytier-core-darwin"
  extract_file "$mac_zip" "easytier-macos-aarch64/easytier-cli" "$BIN_DIR/easytier-cli-darwin"
  chmod +x "$BIN_DIR/easytier-core-darwin" "$BIN_DIR/easytier-cli-darwin"
fi

echo "EasyTier assets ready in $BIN_DIR"
