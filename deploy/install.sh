#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "SafeOps install error: $*" >&2
  exit 1
}

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  fail "must run as root"
fi

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
bundle_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"
[[ -f "$script_dir/install-env.sh" ]] || fail "installer environment helper is missing"
# shellcheck source=install-env.sh
source "$script_dir/install-env.sh"

[[ -f "$bundle_root/VERSION" ]] || fail "bundle VERSION is missing"
[[ -f "$bundle_root/SHA256SUMS" ]] || fail "bundle SHA256SUMS is missing"

kernel="$(uname -s)"
[[ "$kernel" == "Linux" ]] || fail "Linux kernel is required, found $kernel"
architecture="$(uname -m)"
case "$architecture" in
  loongarch64|loong64) ;;
  *) fail "this release requires LoongArch64, found $architecture" ;;
esac

[[ -d /run/systemd/system ]] || fail "systemd is not PID 1 or /run/systemd/system is unavailable"

required_tools=(systemctl journalctl ss ip curl sha256sum getent groupadd useradd install chown chmod od tr cp find basename sleep mktemp mv rm)
for tool in "${required_tools[@]}"; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool is missing: $tool"
done

(
  cd "$bundle_root"
  sha256sum --check SHA256SUMS
) || fail "bundle checksum verification failed"

required_binaries=(
  safeops-server safeops-privexec targetctl
  mcp-system mcp-process mcp-network mcp-journal
  mcp-service mcp-diagnostic mcp-file mcp-config
  safeops-demo-web safeops-port-holder safeops-cpu-hog safeops-log-writer
)
for binary in "${required_binaries[@]}"; do
  [[ -f "$bundle_root/bin/$binary" ]] || fail "required binary is missing: $binary"
done
[[ -f "$bundle_root/web/index.html" ]] || fail "prebuilt frontend index.html is missing"

if ! getent group safeops >/dev/null; then
  groupadd --system safeops || fail "could not create safeops group"
fi
if ! getent passwd safeops >/dev/null; then
  nologin_shell="$(command -v nologin || true)"
  [[ -n "$nologin_shell" ]] || nologin_shell=/usr/sbin/nologin
  useradd --system --gid safeops --home-dir /var/lib/safeops --shell "$nologin_shell" safeops || fail "could not create safeops user"
fi

rm -rf /opt/safeops/web /etc/safeops/knowledge
install -d -m 0755 -o root -g root /opt/safeops /opt/safeops/bin /opt/safeops/web
install -d -m 0750 -o root -g safeops /etc/safeops /etc/safeops/policies /etc/safeops/knowledge
install -d -m 0750 -o safeops -g safeops /var/lib/safeops
install -d -m 0750 -o safeops -g safeops \
  /var/lib/safeops/sessions /var/lib/safeops/tasks /var/lib/safeops/approvals \
  /var/lib/safeops/traces /var/lib/safeops/lab /var/lib/safeops/lab/config
chmod 2750 /var/lib/safeops/approvals
install -d -m 0750 -o root -g safeops /var/lib/safeops/quarantine /run/safeops

for binary in "$bundle_root"/bin/*; do
  [[ -f "$binary" ]] || continue
  install -m 0755 -o root -g root "$binary" "/opt/safeops/bin/$(basename "$binary")"
done

install -m 0640 -o root -g safeops "$bundle_root/config/mcp_servers.yaml" /etc/safeops/mcp_servers.yaml
install -m 0640 -o root -g safeops "$bundle_root/config/executor.yaml" /etc/safeops/executor.yaml
install -m 0640 -o root -g safeops "$bundle_root/policies/tools.yaml" /etc/safeops/policies/tools.yaml
cp -a "$bundle_root/knowledge/." /etc/safeops/knowledge/
find /etc/safeops/knowledge -type d -exec chmod 0755 {} \;
find /etc/safeops/knowledge -type f -exec chmod 0644 {} \;
chown -R root:root /etc/safeops/knowledge
cp -a "$bundle_root/web/." /opt/safeops/web/
find /opt/safeops/web -type d -exec chmod 0755 {} \;
find /opt/safeops/web -type f -exec chmod 0644 {} \;
chown -R root:root /opt/safeops/web

if [[ ! -f /etc/safeops/privexec.hmac ]]; then
  umask 077
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n' > /etc/safeops/privexec.hmac
fi
chown safeops:safeops /etc/safeops/privexec.hmac
chmod 0400 /etc/safeops/privexec.hmac

requested_mode="${SAFEOPS_EXECUTOR_MODE:-}"
if [[ -n "$requested_mode" && "$requested_mode" != "dry-run" && "$requested_mode" != "lab" ]]; then
  fail "SAFEOPS_EXECUTOR_MODE must be dry-run or lab"
fi
safeops_update_env /etc/safeops/safeops.env "$requested_mode" root safeops || fail "could not safely update /etc/safeops/safeops.env"

install -m 0644 -o root -g root "$bundle_root/deploy/systemd/safeops-privexec.service" /etc/systemd/system/safeops-privexec.service
install -m 0644 -o root -g root "$bundle_root/deploy/systemd/safeops-server.service" /etc/systemd/system/safeops-server.service
for unit in "$bundle_root"/lab/systemd/*.service; do
  install -m 0644 -o root -g root "$unit" "/etc/systemd/system/$(basename "$unit")"
done

systemctl daemon-reload || fail "systemctl daemon-reload failed"
systemctl enable safeops-privexec.service safeops-server.service || fail "could not enable SafeOps services"
systemctl restart safeops-privexec.service || fail "safeops-privexec failed to start; inspect journalctl -u safeops-privexec.service"
systemctl restart safeops-server.service || fail "safeops-server failed to start; inspect journalctl -u safeops-server.service"

health_url="http://127.0.0.1:8080/healthz"
for _ in {1..30}; do
  if curl --fail --silent --show-error --max-time 3 "$health_url" >/dev/null; then
    safeops_install_version "$bundle_root/VERSION" /opt/safeops/VERSION root root || fail "could not publish healthy release version metadata"
    version="$(tr -d '\r\n' < /opt/safeops/VERSION)"
    echo "SafeOps $version installed successfully; health: $health_url"
    echo "Executor mode is configured in /etc/safeops/safeops.env; Lab units are installed but not enabled."
    exit 0
  fi
  sleep 1
done

fail "health check failed after 30 seconds; inspect systemctl status safeops-server.service and journalctl -u safeops-server.service"
