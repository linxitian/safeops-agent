#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
# shellcheck source=../deploy/install-env.sh
source "$repo_root/deploy/install-env.sh"

test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail_test() {
  echo "install environment regression failed: $*" >&2
  exit 1
}

assert_file_equals() {
  local expected="$1"
  local actual="$2"
  cmp --silent "$expected" "$actual" || fail_test "unexpected content in $actual"
}

env_file="$test_root/safeops.env"
expected="$test_root/expected.env"
printf '%s\n' \
  '# provider configured by operator' \
  'SAFEOPS_LLM_BASE_URL=https://llm.invalid/v4' \
  'SAFEOPS_EXECUTOR_MODE=dry-run' \
  'SAFEOPS_LLM_API_KEY=test-secret-not-real' \
  'SAFEOPS_EXECUTOR_MODE=lab' \
  'SAFEOPS_LLM_MODEL=test-model' > "$env_file"
before_inode="$(stat -c %i "$env_file")"
success_output="$(safeops_update_env "$env_file" dry-run '' '' 2>&1)"
[[ -z "$success_output" ]] || fail_test "successful update emitted environment data"
printf '%s\n' \
  '# provider configured by operator' \
  'SAFEOPS_LLM_BASE_URL=https://llm.invalid/v4' \
  'SAFEOPS_LLM_API_KEY=test-secret-not-real' \
  'SAFEOPS_LLM_MODEL=test-model' \
  'SAFEOPS_EXECUTOR_MODE=dry-run' > "$expected"
assert_file_equals "$expected" "$env_file"
[[ "$(stat -c %a "$env_file")" == "640" ]] || fail_test "updated file mode is not 0640"
[[ "$(stat -c %i "$env_file")" != "$before_inode" ]] || fail_test "update did not atomically replace the file"

rm -f -- "$env_file"
safeops_update_env "$env_file" '' '' ''
printf '%s\n' 'SAFEOPS_EXECUTOR_MODE=dry-run' > "$expected"
assert_file_equals "$expected" "$env_file"

printf '%s\n' '# keep this comment' 'SAFEOPS_EXECUTOR_MODE=dry-run' 'SAFEOPS_EXECUTOR_MODE=lab' > "$env_file"
safeops_update_env "$env_file" '' '' ''
printf '%s\n' '# keep this comment' 'SAFEOPS_EXECUTOR_MODE=lab' > "$expected"
assert_file_equals "$expected" "$env_file"

printf '%s\n' 'SAFEOPS_EXECUTOR_MODE=unsafe' 'SAFEOPS_LLM_API_KEY=test-secret-not-real' > "$env_file"
cp "$env_file" "$expected"
if failure_output="$(safeops_update_env "$env_file" '' '' '' 2>&1)"; then
  fail_test "invalid existing executor mode was accepted"
fi
[[ "$failure_output" != *test-secret-not-real* ]] || fail_test "failure output exposed an environment value"
assert_file_equals "$expected" "$env_file"

target="$test_root/operator-owned.env"
link="$test_root/symlink.env"
printf '%s\n' 'SAFEOPS_LLM_API_KEY=test-secret-not-real' > "$target"
ln -s "$target" "$link"
if safeops_update_env "$link" lab '' '' >/dev/null 2>&1; then
  fail_test "symlink environment path was accepted"
fi
printf '%s\n' 'SAFEOPS_LLM_API_KEY=test-secret-not-real' > "$expected"
assert_file_equals "$expected" "$target"

version_dir="$test_root/version"
mkdir -p "$version_dir"
version_source="$version_dir/bundle-version"
version_destination="$version_dir/installed-version"
version_owner="$(id -un)"
version_group="$(id -gn)"
printf '%s\n' 'release-1.2.3+test' > "$version_source"
safeops_install_version "$version_source" "$version_destination" "$version_owner" "$version_group"
assert_file_equals "$version_source" "$version_destination"
[[ "$(stat -c %a "$version_destination")" == "644" ]] || fail_test "installed version mode is not 0644"
[[ "$(stat -c %U "$version_destination")" == "$version_owner" ]] || fail_test "installed version owner is incorrect"
[[ "$(stat -c %G "$version_destination")" == "$version_group" ]] || fail_test "installed version group is incorrect"

version_inode="$(stat -c %i "$version_destination")"
printf '%s\n' 'release-1.2.4' > "$version_source"
safeops_install_version "$version_source" "$version_destination" "$version_owner" "$version_group"
assert_file_equals "$version_source" "$version_destination"
[[ "$(stat -c %i "$version_destination")" != "$version_inode" ]] || fail_test "version metadata was not atomically replaced"

cp "$version_destination" "$expected"
printf '%s\n' 'release-ok' 'unexpected-second-line' > "$version_source"
if version_failure="$(safeops_install_version "$version_source" "$version_destination" "$version_owner" "$version_group" 2>&1)"; then
  fail_test "multiline release version was accepted"
fi
[[ "$version_failure" != *unexpected-second-line* ]] || fail_test "invalid version output exposed source content"
assert_file_equals "$expected" "$version_destination"

version_link="$version_dir/version-link"
ln -s "$version_source" "$version_link"
if safeops_install_version "$version_link" "$version_destination" "$version_owner" "$version_group" >/dev/null 2>&1; then
  fail_test "symlink release version source was accepted"
fi
assert_file_equals "$expected" "$version_destination"

printf '%s\n' 'release-1.2.5' > "$version_source"
version_link_target="$version_dir/operator-file"
version_link_destination="$version_dir/version-destination-link"
printf '%s\n' 'operator-content' > "$version_link_target"
cp "$version_link_target" "$expected"
ln -s "$version_link_target" "$version_link_destination"
if safeops_install_version "$version_source" "$version_link_destination" "$version_owner" "$version_group" >/dev/null 2>&1; then
  fail_test "symlink release version destination was accepted"
fi
assert_file_equals "$expected" "$version_link_target"

version_install_line="$(grep -nF 'safeops_install_version "$bundle_root/VERSION" /opt/safeops/VERSION root root' "$repo_root/deploy/install.sh" | cut -d: -f1)"
health_gate_line="$(grep -nF 'if curl --fail --silent --show-error --max-time 3 "$health_url"' "$repo_root/deploy/install.sh" | cut -d: -f1)"
[[ -n "$version_install_line" ]] || fail_test "installer does not install verified VERSION metadata"
[[ -n "$health_gate_line" ]] || fail_test "installer health gate is missing"
[[ "$version_install_line" -gt "$health_gate_line" ]] || fail_test "installer publishes VERSION before the release is healthy"

echo "install environment regression passed"
