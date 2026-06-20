#!/bin/sh
# Installer for the `quick` CLI on macOS and Linux. Downloads the prebuilt
# binary from the latest GitHub Release: no Go required.
#
#   curl -fsSL https://<domain>/install.sh | sh
#
# Variables: QUICK_INSTALL_DIR (default ~/.local/bin).
set -e

repo="zupolgec/quick"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  darwin | linux) ;;
  *) echo "unsupported OS: $os (on Windows use install.ps1)" >&2; exit 1 ;;
esac

url="https://github.com/$repo/releases/latest/download/quick_${os}_${arch}.tar.gz"
dir="${QUICK_INSTALL_DIR:-$HOME/.local/bin}"

echo "Downloading quick ($os/$arch)…"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url" | tar -xz -C "$tmp"
mkdir -p "$dir"
mv "$tmp/quick" "$dir/quick"
chmod +x "$dir/quick"

echo "✓ quick installed in $dir/quick"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "  Add this line to your profile (e.g. ~/.zshrc):"
     echo "    export PATH=\"$dir:\$PATH\"" ;;
esac
echo "  Then: quick login --server https://<your-domain>"
