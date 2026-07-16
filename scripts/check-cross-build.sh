#!/usr/bin/env bash
set -euo pipefail

arch="${1:-loong64}"
case "$arch" in
  amd64|loong64) ;;
  *) echo "unsupported architecture: $arch" >&2; exit 2 ;;
esac

output="bin/linux-$arch"
mkdir -p "$output"
while IFS= read -r package; do
  name="${package##*/}"
  echo "building $package for linux/$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$output/$name" "$package"
done < <(go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./cmd/... | sed '/^$/d' | sort)

