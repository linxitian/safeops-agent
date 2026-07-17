#!/usr/bin/env bash

# safeops_update_env atomically normalizes SAFEOPS_EXECUTOR_MODE while
# preserving every unrelated line. The installer passes the fixed production
# path and ownership; tests pass an isolated path without an ownership change.
safeops_update_env() {
  if [[ "$#" -ne 4 ]]; then
    echo "SafeOps environment update requires file, mode, owner, and group" >&2
    return 1
  fi

  local env_file="$1"
  local requested_mode="$2"
  local env_owner="$3"
  local env_group="$4"
  if [[ "$env_file" != /* ]]; then
    echo "SafeOps environment path must be absolute" >&2
    return 1
  fi
  if [[ -n "$requested_mode" && "$requested_mode" != "dry-run" && "$requested_mode" != "lab" ]]; then
    echo "SafeOps executor mode must be dry-run or lab" >&2
    return 1
  fi
  if [[ -e "$env_file" || -L "$env_file" ]]; then
    if [[ ! -f "$env_file" || -L "$env_file" ]]; then
      echo "SafeOps environment path must be a regular non-symlink file" >&2
      return 1
    fi
  fi

  local env_dir="${env_file%/*}"
  if [[ ! -d "$env_dir" ]]; then
    echo "SafeOps environment directory is missing" >&2
    return 1
  fi

  local old_umask
  old_umask="$(umask)"
  umask 077
  local env_tmp
  if ! env_tmp="$(mktemp "$env_dir/.safeops.env.XXXXXX")"; then
    umask "$old_umask"
    echo "SafeOps environment temporary file creation failed" >&2
    return 1
  fi

  local effective_mode="$requested_mode"
  local line value
  if [[ -f "$env_file" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      if [[ "$line" =~ ^[[:space:]]*SAFEOPS_EXECUTOR_MODE[[:space:]]*= ]]; then
        value="${line#*=}"
        if [[ -z "$requested_mode" ]]; then
          if [[ "$value" =~ ^[[:space:]]*(dry-run|lab)[[:space:]]*$ ]]; then
            effective_mode="${BASH_REMATCH[1]}"
          else
            rm -f -- "$env_tmp"
            umask "$old_umask"
            echo "Existing SafeOps executor mode is invalid" >&2
            return 1
          fi
        fi
        continue
      fi
      if ! printf '%s\n' "$line" >> "$env_tmp"; then
        rm -f -- "$env_tmp"
        umask "$old_umask"
        echo "SafeOps environment preservation failed" >&2
        return 1
      fi
    done < "$env_file"
  fi

  [[ -n "$effective_mode" ]] || effective_mode="dry-run"
  if ! printf 'SAFEOPS_EXECUTOR_MODE=%s\n' "$effective_mode" >> "$env_tmp"; then
    rm -f -- "$env_tmp"
    umask "$old_umask"
    echo "SafeOps environment mode update failed" >&2
    return 1
  fi
  if ! chmod 0640 "$env_tmp"; then
    rm -f -- "$env_tmp"
    umask "$old_umask"
    echo "SafeOps environment permission update failed" >&2
    return 1
  fi
  if [[ -n "$env_owner" || -n "$env_group" ]]; then
    if [[ -z "$env_owner" || -z "$env_group" ]] || ! chown "$env_owner:$env_group" "$env_tmp"; then
      rm -f -- "$env_tmp"
      umask "$old_umask"
      echo "SafeOps environment ownership update failed" >&2
      return 1
    fi
  fi
  if ! mv -fT -- "$env_tmp" "$env_file"; then
    rm -f -- "$env_tmp"
    umask "$old_umask"
    echo "SafeOps environment atomic replacement failed" >&2
    return 1
  fi
  umask "$old_umask"
}
