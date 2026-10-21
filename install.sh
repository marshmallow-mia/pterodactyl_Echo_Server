#!/bin/sh
# Installs evr-ptero from GitHub releases and starts the setup.
#   curl -fsSL https://github.com/marshmallow-mia/pterodactyl_Echo_Server/releases/latest/download/install.sh | sudo sh
# Set EVR_VERSION=v2.0.0 to pick a version, EVR_NO_INIT=1 to only install.
set -eu
repo="marshmallow-mia/pterodactyl_Echo_Server"
version="${EVR_VERSION:-latest}"
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
[ "$(id -u)" = 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
if [ "$version" = latest ]; then base="https://github.com/$repo/releases/latest/download"
else base="https://github.com/$repo/releases/download/$version"; fi
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
echo "Downloading evr-ptero ($version, linux/$arch)…"
curl -fsSL -o "$tmp/evr-ptero_linux_$arch" "$base/evr-ptero_linux_$arch"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
(cd "$tmp" && grep " evr-ptero_linux_$arch\$" checksums.txt | sha256sum -c -) >/dev/null || { echo "checksum mismatch" >&2; exit 1; }
install -m 0755 "$tmp/evr-ptero_linux_$arch" /usr/local/bin/evr-ptero
echo "Installed /usr/local/bin/evr-ptero ($(/usr/local/bin/evr-ptero --version))"
if [ ! -f /var/www/pterodactyl/artisan ] && [ ! -f /var/www/pelican/artisan ]; then
  echo 'No panel on this host yet: "evr-ptero init" can install Pterodactyl or Pelican with Wings,'
  echo 'or connect to a panel elsewhere.'
fi
command -v rsync >/dev/null 2>&1 || echo "Tip: install rsync to download the game files (apt install rsync)."
if [ -z "${EVR_NO_INIT:-}" ] && [ -t 1 ] && [ -r /dev/tty ]; then
  exec /usr/local/bin/evr-ptero init < /dev/tty
fi
echo "Next: evr-ptero init"
