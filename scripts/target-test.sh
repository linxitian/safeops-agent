#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

arch="$(uname -m)"
case "$arch" in
  loongarch64|loong64) candidate="bin/linux-loong64/targetctl" ;;
  x86_64|amd64) candidate="bin/linux-amd64/targetctl" ;;
  *) echo "unsupported native architecture: $arch" >&2; exit 2 ;;
esac

if [[ -x "bin/targetctl" ]]; then
  candidate="bin/targetctl"
fi
if [[ ! -x "$candidate" ]]; then
  echo "targetctl binary not found: $candidate" >&2
  exit 2
fi

exec "$candidate" test \
  --mcp-config "./config/mcp_servers.yaml" \
  --output-dir "./artifacts/target"
