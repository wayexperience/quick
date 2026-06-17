#!/bin/sh
# Installer della CLI `quick` per macOS e Linux. Scarica il binario già
# compilato dall'ultima GitHub Release: nessun Go richiesto.
#
#   curl -fsSL https://<dominio>/install.sh | sh
#
# Variabili: QUICK_INSTALL_DIR (default ~/.local/bin).
set -e

repo="zupolgec/quick"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "architettura non supportata: $arch" >&2; exit 1 ;;
esac
case "$os" in
  darwin | linux) ;;
  *) echo "OS non supportato: $os (su Windows usa install.ps1)" >&2; exit 1 ;;
esac

url="https://github.com/$repo/releases/latest/download/quick_${os}_${arch}.tar.gz"
dir="${QUICK_INSTALL_DIR:-$HOME/.local/bin}"

echo "Scarico quick ($os/$arch)…"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url" | tar -xz -C "$tmp"
mkdir -p "$dir"
mv "$tmp/quick" "$dir/quick"
chmod +x "$dir/quick"

echo "✓ quick installato in $dir/quick"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "  Aggiungi questa riga al tuo profilo (es. ~/.zshrc):"
     echo "    export PATH=\"$dir:\$PATH\"" ;;
esac
echo "  Poi: quick login --server https://<il-tuo-dominio>"
