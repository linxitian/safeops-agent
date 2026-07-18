# SafeOps Release Deployment

The LoongArch64 release archive is self-contained. Verify the outer `.sha256`, extract it, then run `deploy/install.sh` as root on the target. The installer verifies the bundle-level `SHA256SUMS` before changing the host.

## Official Kylin VM evidence workflow

Run this only inside the official LoongArch64 Kylin V11 VM from the directory containing the archive and its `.sha256`:

```bash
sha256sum --check safeops-agent-linux-loong64.tar.gz.sha256
tar -xzf safeops-agent-linux-loong64.tar.gz
cd safeops-agent
sha256sum --check SHA256SUMS
sudo SAFEOPS_EXECUTOR_MODE=lab ./deploy/install.sh
curl --fail --silent --show-error http://127.0.0.1:8080/healthz
sudo -u safeops /opt/safeops/bin/targetctl probe \
  --output-dir /var/lib/safeops/target-report
sudo -u safeops /opt/safeops/bin/targetctl test \
  --mcp-config /etc/safeops/mcp_servers.yaml \
  --output-dir /var/lib/safeops/target-report
sudo -u safeops /opt/safeops/bin/targetctl doctor \
  --mcp-config /etc/safeops/mcp_servers.yaml \
  --output-dir /var/lib/safeops/target-report
```

Open or forward `http://127.0.0.1:8080`, then follow `lab/README.md`. Return the four files below, relevant Task/Trace exports or screenshots, and the exact release SHA-256:

```text
/var/lib/safeops/target-report/target-probe-report.json
/var/lib/safeops/target-report/target-probe-report.txt
/var/lib/safeops/target-report/target-test-report.json
/var/lib/safeops/target-report/target-test-report.txt
```

The generated report intentionally keeps `target_verified=false` until maintainers audit the returned official-VM identity, native checks and Demo evidence together. Do not self-promote the status from a cross-build or an unaudited report.

By default the privileged executor uses `dry-run`; the server and all MCP subprocesses run as the non-root `safeops` user. To enable only the fixed controlled Lab handlers during an authorized Demo installation:

```bash
sudo SAFEOPS_EXECUTOR_MODE=lab ./deploy/install.sh
```

The installer enables and starts `safeops-privexec.service` and `safeops-server.service`, then checks `http://127.0.0.1:8080/healthz`. It installs the four Lab units but does not enable or start them.

The API process refuses non-loopback listen addresses because it does not implement remote authentication. The packaged unit also makes the runtime limits explicit: 8 concurrent Agent runs or approval resumes, 1,000 retained Sessions and 10,000 retained Tasks. Concurrent saturation returns HTTP 429; exhausted retention capacity returns HTTP 507. Neither path changes durable state. Use an authenticated local proxy or controlled SSH forwarding for remote operator access.

On reinstall, the installer atomically updates only `SAFEOPS_EXECUTOR_MODE` in `/etc/safeops/safeops.env`. It preserves operator comments and unrelated settings such as the supported `SAFEOPS_LLM_*` variables, normalizes duplicate executor-mode entries to one authoritative value, and restores `root:safeops` ownership with mode `0640`. Existing symlinks and malformed executor modes fail closed.

Uninstall preserves `/var/lib/safeops` by default:

```bash
sudo ./deploy/uninstall.sh
```

Use `--purge-data` only when durable sessions, tasks, approvals, traces, quarantine manifests/files and Lab data may be permanently removed.

The default uninstaller intentionally removes `/etc/safeops` as configuration while preserving `/var/lib/safeops` as durable data. If a later reinstall must retain the compatible-provider environment and the signing identity used by approval-bound envelopes, save only those two files in a root-only location before uninstall and restore them before reinstall:

```bash
sudo install -d -m 0700 -o root -g root /root/safeops-config-backup
sudo install -m 0600 -o root -g root \
  /etc/safeops/safeops.env /root/safeops-config-backup/safeops.env
sudo install -m 0600 -o root -g root \
  /etc/safeops/privexec.hmac /root/safeops-config-backup/privexec.hmac

sudo ./deploy/uninstall.sh

sudo install -d -m 0750 -o root -g safeops /etc/safeops
sudo install -m 0640 -o root -g safeops \
  /root/safeops-config-backup/safeops.env /etc/safeops/safeops.env
sudo install -m 0400 -o safeops -g safeops \
  /root/safeops-config-backup/privexec.hmac /etc/safeops/privexec.hmac
sudo SAFEOPS_EXECUTOR_MODE=lab ./deploy/install.sh
```

Never copy either file into a user-readable evidence directory or commit it. Without this optional backup, reinstall creates a new HMAC and has no operator LLM configuration, which is the expected behavior after configuration removal.
