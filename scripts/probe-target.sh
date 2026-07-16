#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [[ -x "bin/targetctl" ]]; then
  exec bin/targetctl probe
fi

arch="$(uname -m)"
case "$arch" in
  loongarch64|loong64) exec bin/linux-loong64/targetctl probe ;;
  x86_64|amd64) exec bin/linux-amd64/targetctl probe ;;
  *) echo "unsupported native architecture: $arch" >&2; exit 2 ;;
esac
