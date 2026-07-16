#!/usr/bin/env bash
set -euo pipefail

arch="${1:-loong64}"
case "$arch" in
  amd64|loong64) ;;
  *) echo "unsupported architecture: $arch" >&2; exit 2 ;;
esac

output="bin/linux-$arch"
mkdir -p "$output"
packages="$(mktemp)"
trap 'rm -f "$packages"' EXIT
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./cmd/... | sed '/^$/d' | sort > "$packages"
if [ ! -s "$packages" ]; then
  echo "no Go command packages discovered under ./cmd" >&2
  exit 1
fi
while IFS= read -r package; do
  name="${package##*/}"
  echo "building $package for linux/$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$output/$name" "$package"
done < "$packages"
