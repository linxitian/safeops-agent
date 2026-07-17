# Kylin V11 LoongArch64 Target Verification Audit

Audit date: 2026-07-18. Maintainer verdict: the evidence below is sufficient to promote only the named capabilities to `TARGET_VERIFIED`.

## Release identity

- Merged commit: `1a10880fd8ae200eb3fa9964943e4112f4cbf6a8` (PR #14).
- Release version embedded in the final installed bundle: `1a10880`.
- `safeops-agent-linux-loong64.tar.gz` SHA-256: `4af422c722d267b24efa3f7f16d2536f9ee475dd99326ba2d9247d13e1283945`.
- The outer checksum and all bundle `SHA256SUMS` entries passed before installation.
- The final bundle passed `go test ./...`, `go vet ./...`, the installer regression, 14 frontend tests, frontend lint/build, and all 16 commands for both linux/amd64 and linux/loong64 with `CGO_ENABLED=0`.

## Official target identity and native reports

The target is the official project VM reached as `safeops-kylin`. The three generated report checksums were independently rechecked before this audit.

| Scope | Report ID | Native result | Audit result |
|---|---|---|---|
| probe | `target_0f70b837d6ab5d7c72bd` | 17/19 PASS | accepted |
| test | `target_3ca1769f7eb00323a1d8` | 28/30 PASS | accepted |
| doctor | `target_d05c9711293c083a26c2` | 31/33 PASS | accepted |

The reports identify Kylin Linux Advanced Server V11 (Swan25), `linux/loong64`, kernel `6.6.0-32.7.v2505.ky11.loongarch64`, glibc 2.38 and systemd 255. The installed binary reports Go 1.26.4 and `CGO_ENABLED=0`. All eight MCP servers initialized and answered ping, all 39 tools were discovered, and the real structured `/proc` memory call passed.

The overall report result is `WARN` only because the target image does not install the optional `git` and `go` commands. GCC, `ip`, `ss`, `systemctl`, `journalctl`, procfs, statfs, Kylin identity and native LoongArch64 checks passed. The missing development commands do not invalidate the self-contained release runtime.

Generated reports intentionally retain `target_verified=false`. That field prevents self-promotion by an unaudited target run. This document records the separate maintainer audit and does not alter the generated artifacts.

## Complete MCP native-call follow-up

Runtime commit `b5383e9` extended the target test from one representative memory call to a unique, individually time-bounded call for every discovered Tool. Its full release gates passed, the LoongArch64 archive SHA-256 was `380660f64e08936f3f0581f94400bf3cedc3b51916ec21ea82cf706971b32076`, and the exact bundle was installed before the final run.

The first expanded target run exposed that the unquoted comma in the YAML `mcp-config` root argument left `/var/lib/safeops/lab/config` outside the effective allowlist. Issues #19 and #20 record the coverage gap and manifest defect. Both development and installed manifests now preserve `/etc/safeops,/var/lib/safeops/lab/config` as one argument, with a load-time regression test for each file.

The installed `/opt/safeops/bin/targetctl` then ran as the non-root `safeops` identity against `/etc/safeops/mcp_servers.yaml`. Checksum-verified report `target_ae6d4bbeb9ae7b8e5764` recorded 8/8 healthy/pinged Servers, 39/39 discovered Tools, a unique 39/39 call plan and successful structured results for all 39 calls. This includes the previously discovery-only system, process, network, journal, service, diagnostic, file and configuration calls. The test uses its own PID for process identity, installed service/loopback targets, an existing bounded Lab file and a non-secret Lab configuration snapshot baseline. It records no successful tool payload or configuration content; errors are bounded and redact common credential forms.

The report remains `WARN` solely for the same optional `git`/`go` command absence and intentionally retains `target_verified=false`. Maintainer audit of the release identity, report checksum and per-tool checks promotes all 39 matrix entries to `CALLED` without changing that self-protection field.

## Installed workflow evidence

Every task below was read back from the target durable store and every exported Trace reported `VALID` integrity.

| Capability | Target task | Result | Trace facts |
|---|---|---|---|
| credentialed compatible-provider read | `task_bbb4e7783584ab520a9e30ca` | `COMPLETED`, 1/1 | 15 events, `FINAL` |
| port-conflict recovery | `task_eaa766483656e0582748ccfc` | `COMPLETED`, 10/10 | 87 events, two approvals, `FINAL` |
| CPU-hog recovery | `task_7cd27a9c979c9c885bc791fe` | `COMPLETED`, 7/7 | 67 events, exact PID approval, `FINAL` |
| disk/log-growth recovery | `task_d6a7533a6843490c7c6fa872` | `COMPLETED`, 8/8 | 78 events, separate process/file approvals, `FINAL` |
| prompt-injection negative | `task_59ee56cbb70f23255424c621` | expected `FAILED` | 12 events; no tool, approval or execution |
| final large-file discovery | `task_ca29dca6b12d0e9538f75511` | `COMPLETED`, 2/2 | 23 events, ordered three-file selection, `FINAL` |
| final contextual recommendation | `task_017867499b071ab9a5cc252e` | `COMPLETED`, 3/3 | 31 events; stat of only the three selected files, `FINAL` |
| final ordinal quarantine | `task_9a5e1b6c7d784a7d1edafb8a` | `COMPLETED`, 1/1 | 17 events, exact third resource, `FINAL` |
| final ordinal restore | `task_c9eed468c1e8abd27cb9c025` | `COMPLETED`, 1/1 | 19 events, same quarantine record, `FINAL` |

The final four-turn Session ran on merged release `1a10880`. It selected `demo-3.log`, `demo-2.log`, and `demo-1.log` in descending order; the ambiguous follow-up performed `file.stat` on those three resources only. “第三个” resolved to `demo-1.log`, which was approval-bound, quarantined, restored to its original 2 MiB path, owner and mode, and removed from quarantine. The 3 MiB and 4 MiB files were unchanged.

The port, CPU, disk and injection tasks were first executed on ancestor release `c4e8e7c`; their durable tasks and hash chains were re-read and verified after installing final release `1a10880`. The relevant workflow code was unchanged by PR #14, whose changes were limited to general follow-up context and provider deadlines.

## Deployment and durability evidence

- `safeops-server` and all MCP subprocesses ran as the non-root `safeops` user.
- Privileged actions crossed the Unix socket to fixed Lab handlers; no arbitrary shell or command tool was introduced.
- Install, service start, `/healthz`, repeated reinstall and LLM environment preservation passed on the target.
- Follow-up release `0b88b6f` (a documentation-only descendant of the audited runtime) produced archive SHA-256 `2c0abb54d43c243370ccb0730917d01e30db55f8845f7db1e0598fa3d58ec8cc` and completed the default target uninstall/reinstall test.
- After stopping the two core services, the default uninstaller removed `/opt/safeops`, `/etc/safeops` and all six units while retaining the `safeops` identity. The 140-file SHA-256 manifest and 153-row metadata manifest for `/var/lib/safeops` were byte-identical before and after uninstall.
- A root-only backup restored `safeops.env` and `privexec.hmac` before reinstall. Health returned 8/8 MCP and 39 tools; 15 Sessions, 29 Tasks and 17 approvals remained readable; a retained 19-event Trace was still `VALID`/`FINAL`; provider and HMAC continuity passed.
- A slow-provider task that previously left task/Trace state inconsistent was recovered after the deadline fix to durable `FAILED`, with its lease released and Trace still `VALID`.
- Lab generator units were stopped after testing. The restored files matched their original sizes and the final quarantine object was absent.

## Status decisions and remaining gaps

The audited native evidence promotes all 39 MCP read-tool calls/runtime, general provider interaction, safety/approval/executor/rollback, evidence/RCA, hash Trace, port/CPU/disk/file workflows, and Kylin/LoongArch64 compatibility to `TARGET_VERIFIED`.

Release/deployment is also `TARGET_VERIFIED` for checksum verification, install, start, health, repeated reinstall, default data-preserving uninstall and configuration-continuity restore. The uninstaller intentionally removes `/etc/safeops`; retaining provider and approval-signing identity requires the root-only procedure in `deploy/README.md`.

The following remain below `TARGET_VERIFIED`:

- the seven-Collector abstraction remains `TESTED` because `targetctl` does not individually execute every Collector adapter;
- the six-page UI remains `TESTED`; the installed console was served and used, but a systematic target-browser traversal/accessibility run was not captured;
- benchmarks remain `TESTED` on the development host and are not treated as target evidence;
- target `git` and `go` commands remain absent, although they are not runtime dependencies.
