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

# safeops_validate_version_identifier is shared by the release builder and the
# target installer so an archive cannot be produced with an identity the target
# would reject.
safeops_validate_version_identifier() {
  if [[ "$#" -ne 1 ]]; then
    echo "SafeOps version validation requires one identifier" >&2
    return 1
  fi
  if [[ ! "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$ ]]; then
    echo "SafeOps version must be one bounded release identifier" >&2
    return 1
  fi
}

# safeops_validate_version_source performs the file safety and release identity
# checks without changing the host. The installer calls it during preflight and
# the final publisher calls it again immediately before atomic replacement.
safeops_validate_version_source() {
  if [[ "$#" -ne 1 ]]; then
    echo "SafeOps version validation requires one source" >&2
    return 1
  fi

  local version_source="$1"
  if [[ "$version_source" != /* ]]; then
    echo "SafeOps version source path must be absolute" >&2
    return 1
  fi
  if [[ ! -f "$version_source" || -L "$version_source" ]]; then
    echo "SafeOps version source must be a regular non-symlink file" >&2
    return 1
  fi
  if [[ -e "$version_destination" || -L "$version_destination" ]]; then
    if [[ ! -f "$version_destination" || -L "$version_destination" ]]; then
      echo "SafeOps version destination must be a regular non-symlink file" >&2
      return 1
    fi
  fi

  local version_lines=()
  mapfile -t version_lines < "$version_source"
  if [[ "${#version_lines[@]}" -ne 1 ]]; then
    echo "SafeOps version must be one bounded release identifier" >&2
    return 1
  fi
  safeops_validate_version_identifier "${version_lines[0]}"
}

# safeops_install_version validates and atomically installs the public release
# identity. The installer supplies root ownership; tests use the current user
# inside an isolated directory.
safeops_install_version() {
  if [[ "$#" -ne 4 ]]; then
    echo "SafeOps version install requires source, destination, owner, and group" >&2
    return 1
  fi

  local version_source="$1"
  local version_destination="$2"
  local version_owner="$3"
  local version_group="$4"
  if [[ "$version_destination" != /* ]]; then
    echo "SafeOps version destination path must be absolute" >&2
    return 1
  fi
  safeops_validate_version_source "$version_source" || return 1
  if [[ -e "$version_destination" || -L "$version_destination" ]]; then
    if [[ ! -f "$version_destination" || -L "$version_destination" ]]; then
      echo "SafeOps version destination must be a regular non-symlink file" >&2
      return 1
    fi
  fi

  local version_dir="${version_destination%/*}"
  if [[ ! -d "$version_dir" ]]; then
    echo "SafeOps version destination directory is missing" >&2
    return 1
  fi
  local version_tmp
  if ! version_tmp="$(mktemp "$version_dir/.VERSION.XXXXXX")"; then
    echo "SafeOps version temporary file creation failed" >&2
    return 1
  fi
  if ! install -m 0644 -o "$version_owner" -g "$version_group" -- "$version_source" "$version_tmp"; then
    rm -f -- "$version_tmp"
    echo "SafeOps version metadata install failed" >&2
    return 1
  fi
  if ! mv -fT -- "$version_tmp" "$version_destination"; then
    rm -f -- "$version_tmp"
    echo "SafeOps version atomic replacement failed" >&2
    return 1
  fi
}
