#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"
# shellcheck source=../deploy/install-env.sh
source "$repo_root/deploy/install-env.sh"

go_bin="${GO:-go}"
npm_bin="${NPM:-npm}"
release_dir="$repo_root/dist/release"
build_dir="$repo_root/dist/build-release"
bundle="$build_dir/safeops-agent"
archive_name="safeops-agent-linux-loong64.tar.gz"
version="${SAFEOPS_VERSION:-}"
if [[ -z "$version" ]]; then
  version="$(git describe --tags --always --dirty 2>/dev/null || true)"
fi
[[ -n "$version" ]] || version="0.1.0-dev"
safeops_validate_version_identifier "$version" || {
  echo "[release] SAFEOPS_VERSION is invalid" >&2
  exit 1
}

echo "[release] Go test"
CGO_ENABLED=0 "$go_bin" test ./...
echo "[release] Go vet"
CGO_ENABLED=0 "$go_bin" vet ./...
echo "[release] Installer environment regression"
"$repo_root/scripts/test-install-env.sh"
echo "[release] Frontend component/accessibility tests"
"$npm_bin" --prefix web test
echo "[release] Frontend lint"
"$npm_bin" --prefix web run lint
echo "[release] Frontend build"
"$npm_bin" --prefix web run build

rm -rf "$build_dir" "$release_dir"
mkdir -p "$bundle/bin" "$bundle/config" "$bundle/policies" "$bundle/knowledge" \
  "$bundle/web" "$bundle/deploy/systemd" "$bundle/lab/systemd" "$release_dir"

build_commands() {
  local architecture="$1"
  local output="$build_dir/linux-$architecture"
  mkdir -p "$output"
  while IFS= read -r package; do
    local name="${package##*/}"
    echo "[release] build $package for linux/$architecture"
    CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" "$go_bin" build -trimpath -o "$output/$name" "$package"
  done < <("$go_bin" list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./cmd/... | sed '/^$/d' | sort)
}

build_commands amd64
build_commands loong64

cp -a "$build_dir/linux-loong64/." "$bundle/bin/"
cp deploy/config/mcp_servers.yaml "$bundle/config/mcp_servers.yaml"
cp config/executor.yaml "$bundle/config/executor.yaml"
cp policies/tools.yaml "$bundle/policies/tools.yaml"
cp -a knowledge/. "$bundle/knowledge/"
cp -a web/dist/. "$bundle/web/"
cp deploy/install.sh deploy/install-env.sh deploy/uninstall.sh deploy/README.md "$bundle/deploy/"
cp deploy/systemd/*.service "$bundle/deploy/systemd/"
cp lab/systemd/*.service "$bundle/lab/systemd/"
cp lab/README.md "$bundle/lab/README.md"
cp README.md STATUS.md "$bundle/"
chmod 0755 "$bundle/deploy/install.sh" "$bundle/deploy/uninstall.sh"

printf '%s\n' "$version" > "$bundle/VERSION"

(
  cd "$bundle"
  while IFS= read -r -d '' file; do
    sha256sum "$file"
  done < <(find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z)
) | sed 's#  \./#  #' > "$bundle/SHA256SUMS"

tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
  -C "$build_dir" -czf "$release_dir/$archive_name" safeops-agent
(
  cd "$release_dir"
  sha256sum "$archive_name" > "$archive_name.sha256"
)

echo "[release] created $release_dir/$archive_name"
echo "[release] created $release_dir/$archive_name.sha256"
