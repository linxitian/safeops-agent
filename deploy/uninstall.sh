#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "SafeOps uninstall error: $*" >&2
  exit 1
}

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  fail "must run as root"
fi

purge_data=false
case "${1:-}" in
  "") ;;
  --purge-data) purge_data=true ;;
  *) fail "usage: $0 [--purge-data]" ;;
esac

units=(
  safeops-server.service safeops-privexec.service
  safeops-demo-web.service safeops-port-holder.service
  safeops-cpu-hog.service safeops-log-writer.service
)

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now "${units[@]}" >/dev/null 2>&1 || true
fi

for unit in "${units[@]}"; do
  rm -f "/etc/systemd/system/$unit"
done
rm -rf /opt/safeops /etc/safeops

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || fail "systemctl daemon-reload failed"
  systemctl reset-failed >/dev/null 2>&1 || true
fi

if [[ "$purge_data" == true ]]; then
  rm -rf /var/lib/safeops
  if getent passwd safeops >/dev/null 2>&1; then
    userdel safeops || fail "could not remove safeops user"
  fi
  if getent group safeops >/dev/null 2>&1; then
    groupdel safeops || fail "could not remove safeops group"
  fi
  echo "SafeOps removed, including durable data."
else
  echo "SafeOps binaries/configuration removed; /var/lib/safeops was preserved."
  echo "Run $0 --purge-data only when durable sessions, approvals, traces, quarantine and Lab data may be deleted."
fi
