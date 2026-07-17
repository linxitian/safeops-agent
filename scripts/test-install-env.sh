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

echo "install environment regression passed"
