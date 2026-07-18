# Kylin V11 LoongArch64 Target Verification Audit

Audit date: 2026-07-18, with a real-agent reliability follow-up on 2026-07-19. Maintainer verdict: the evidence below is sufficient to promote only the named capabilities to `TARGET_VERIFIED`.

Evidence identities, candidate-to-merge mappings, checksums, and the boundary
between Git-tracked material and the maintainers' restricted raw archive are
indexed in [`docs/evidence`](evidence/README.md). Raw reports, Task/Trace
exports, browser captures, screenshots, and release archives are not tracked
in this repository; a fresh clone can validate default-branch identities and
retrieve the recorded candidate lineage through public PR refs, but cannot
recompute external artifact hashes without obtaining those artifacts.

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

Candidate runtime `b5383e9` extended the target test from one representative memory call to a unique, individually time-bounded call for every discovered Tool. It is an intermediate ancestor of final PR #21 head `a1ce22f`, not a commit reachable from `main`; the PR was squash-merged as `816f8cf`. The public pull ref and all three full identities are recorded in the evidence manifest. Its full release gates passed, the LoongArch64 archive SHA-256 was `380660f64e08936f3f0581f94400bf3cedc3b51916ec21ea82cf706971b32076`, and the exact candidate bundle was installed before the final run.

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

## Installed browser audit follow-up

Candidate runtime `2b26de4` added an explicit SVG favicon to the bundled Web assets and an API regression test for its MIME type and security headers. It is an intermediate ancestor of final PR #23 head `04f1a91`, not a commit reachable from `main`; the PR was squash-merged as `7479752`. Its complete release gates passed, the LoongArch64 archive SHA-256 was `0accff7af8ad7eaecabe4e262f4cbc1fa6caa9359f34615e5927174af8d135f8`, and that exact candidate bundle was checksum-verified and installed on the target before browser capture.

A clean real Google Chrome session on the Ubuntu operator host traversed the target-served UI through a fixed read-only SSH proxy to target loopback. The proxy accepted only a bounded GET/HEAD allowlist; it did not provide any write or arbitrary-command surface. Console, Overview, Tool, Safety, RCA, Audit, Allowlist and LLM views each reached their expected heading and loaded target-backed data. Overview showed 8/8 healthy MCP Servers and 39 discovered Tools; the Tool page rendered eight Server cards; Allowlist loaded the active managed roots; LLM showed the configured Provider/model while the secret field remained empty and no key was recorded.

Chrome recorded 39 responses, all HTTP 200, with no console warnings/errors, JavaScript exceptions, failed responses or loading failures. Every view had zero visible errors, zero horizontal overflow, zero unnamed visible DOM controls and zero unnamed Chrome Accessibility Tree controls. The audit JSON SHA-256 is `0e3d98cf68c694a58300e8c459b5eeb9bbfd796be0d8c0734a09de303d831101`; eight full-page PNG captures accompany the local evidence set. This evidence closes the prior systematic browser-traversal gap and promotes M13/B/S architecture to `TARGET_VERIFIED` for the installed target-served UI.

## Exact merged release and native benchmark follow-up

PR #23 merged the favicon/browser-audit changes as `7479752df7d29272f3a08441b26a887b8bb388e0`. A fresh release from that exact merge passed Go test/vet, the installer regression, 14 frontend tests, frontend lint/build and all 16 commands for linux/amd64 and linux/loong64 with `CGO_ENABLED=0`. The LoongArch64 archive SHA-256 was `6b2aa3220af27ac24a5c407be9df23ab82e203a878913d4a414868dc76d910a8`.

That archive was checksum-verified and reinstalled on the official target. The installed server and favicon hashes matched the extracted bundle, both core services were active, the favicon returned HTTP 200 with `image/svg+xml`, CSP and `nosniff`, and the four fault-generator Lab units remained inactive. The configured Provider metadata remained readable without returning the key value.

Installed `targetctl test` then ran as non-root `safeops`. Report `target_7a42c5e387e7abeea4f8` recorded 8/8 Server discovery/ping and one successful structured call for each of 39/39 Tools. It remained `WARN` only for optional target `git`/`go` command absence. The JSON and text report SHA-256 values were `92847f3820226913b8d3863be5facc73d0b92cf6bdca9634988c7431ca83487c` and `2c5d3f7175e64e7c75ee6cd4eaadb04a8625ab3d064098e00717ebf0f1c9aba6`.

The installed `safeops-bench all` next ran as non-root `safeops` with the installed policy. All six suites passed and all 16 metrics were measured on `linux/loong64`: intent 7/7, ordered tool selection and completion 7/7, high-risk denial 5/5, unauthorized execution 0/5, safety false positives 0/5, RCA Top-1/Top-3 5/5, context resolution 6/6, approval resume 2/2, restart recovery 1/1, Trace coverage 12/12, Trace integrity 2/2 and real scoped temporary-file rollback 3/3. The 2,000-call native RCA sample measured a 0.025 ms mean and 0.036 ms P95 on this VM. JSON and Markdown SHA-256 values were `03538c50a122b44a71f2e6adcc651a104a32a2fcc0e124afba703a8813a9dc24` and `9dcbc364e112dc135b6d5b1725caf89bac3ea1076b4fb75ae2529051c707b78d`; all `safeops-bench-*` temporary directories were removed.

The benchmark dangerous-target cases evaluate the production Guard only; execution cases use a DryRun handler, and real quarantine/restore is confined to generated temporary files. This evidence promotes M15 to `TARGET_VERIFIED` for controlled native execution. It does not turn fixture scores into real-world accuracy estimates, and the latency values are specific to this target environment.

## Native Collector and adapter follow-up

Issue #26 tracked the missing target execution evidence for the seven production Collectors. Candidate runtime `053fc2c` is an intermediate ancestor of final PR #27 head `504faef`, not a commit reachable from `main`; the PR was squash-merged as `4861dcf`. It passed Go test/vet, the installer environment regression, 14 frontend tests, frontend lint/build and all 16 commands for linux/amd64 and linux/loong64 with `CGO_ENABLED=0`. Its LoongArch64 archive SHA-256 was `d342dd4ed374cfe1b0f2145dd55dbf7eb8ce9c38e77a901a4b102e554a574ef8`.

The first native candidate correctly failed because the allowlisted Lab configuration directory was empty: six Collectors and both adapters passed, while `config_change` returned no valid observations. The follow-up kept empty results fail-closed, added a bounded SafeFS snapshot for a single allowlisted file, and pointed the production configuration Collector at the installed, non-secret `/etc/safeops/mcp_servers.yaml` manifest. It reads only metadata and a bounded hash in memory; neither the hash nor any Observation value is written to the target report.

After checksum verification and reinstall, `/opt/safeops/bin/targetctl test` ran as non-root `safeops`. Report `target_df68d477155fd8d55d75` recorded the exact 7/7 unique Collector plan, 7/7 completed, 195 observations, zero issues and no output/issue truncation. Per-Collector counts passed for procfs (29), disk (47), network (36), systemd (14), journal (20), system configuration (46) and configuration change (3). The Prometheus model produced 62 numeric samples and skipped 133 non-numeric observations; the OpenTelemetry model produced 62 metrics and 133 bounded logs. Neither adapter truncated.

The same report repeated 8/8 MCP health and 39/39 structured native Tool calls. Its JSON and text SHA-256 values were `cb550695d5d3af42cc30ec55cab0500ff68b79b0f86bee8836438e27affadf77` and `45af6344f28da0aafb317370183ea33f71c5369d760710259fc9b10192e69f15`. A field-pattern audit found no persisted Observation value, configuration hash or credential field. The server health endpoint returned HTTP 200, both core units were active and all four fault-generator Lab units remained inactive.

The report remained `WARN` only because the target lacks optional `git` and `go`, and retained `target_verified=false` by design. Maintainer review of the exact release identity, first fail-closed run, corrected native run and count-only artifact promotes M1 to `TARGET_VERIFIED`. This evidence covers in-process Prometheus/OpenTelemetry adaptation; no external telemetry backend was deployed or claimed.

## Exact PR #30 merged-runtime regression

Merge commit `666df77385ece217c2fec2eca48a1007762b6d04` was built after PR #30 with the full Go, frontend, installer, and dual-architecture release gates. The LoongArch64 archive SHA-256 was `33faf74629334318a15f63726baae233455206ab5379a0d7337406300a8b674f`, and the installed bundle reported version `666df77`.

Native report `target_4a368ebd750533d7ddb6` then completed 7/7 Collectors with 200 observations, both adapter models, 8/8 MCP servers, and 39/39 structured Tool calls. Its JSON and text SHA-256 values were `7469aac4b1bc6ff788b5699008122b20ecc873aa1a2ecdab1f2e1a0186048a67` and `e21bbb10bcca77ac0d9d009542f334b6f54ba508263498e6b3995cc748909162`. The result remained `WARN` only for optional target `git`/`go` absence and retained `target_verified=false`.

This is exact-merge native runtime regression evidence for the named Collector, adapter, Registry, and MCP-call checks. It does not exercise the positive/negative M17 guarded-LLM action flows and therefore does not promote M17 beyond `TESTED`. Later merges #34, #36, #38, and #40 require their own exact-merge target release if a new target-compatibility claim is needed.

## PR #57 real-agent reliability follow-up

Candidate `96e6702cadc88ab9b243b9fb1db6240d2c8698da` passed `go test ./...`,
`go vet ./...`, the installer environment regression, 19 frontend tests,
frontend lint/build, and all 16 commands for both linux/amd64 and
linux/loong64 with `CGO_ENABLED=0`. Its LoongArch64 release archive SHA-256
was `351ff342e1b783e092dd28a90b4efe4ba9f3e0a174c421a5fa23cf47bbcb3650`.
The checksum-verified bundle was installed on the official Kylin target;
`/opt/safeops/VERSION` reported `96e6702`, both core services were active,
8/8 MCP servers were `HEALTHY`, and all four fault-generator Lab units
remained inactive.

The operator changed the persisted OpenAI-compatible Provider to
`https://api.deepseek.com` with model `deepseek-v4-flash`; the secret value
was neither read back nor recorded. Target task
`task_a054a5ffc7680ae9016682c4` then handled `修复 /var/log 磁盘占用问题`
in 10.1 seconds and completed after one scoped `system.get_disk_usage` call on
`/var/log`. Its final answer converted the structured byte counts correctly
to GiB, reported 13.9% filesystem use, concluded that no disk
pressure required repair, and cited the exact evidence reference. The
exported Trace was `VALID`.

This run followed three evidence-producing failures that shaped PR #57:
`task_6987e27baae5b9b0a770f433` failed on the misspelled field
`expected_observaton`; `task_363660771fa02836af64cc6e` gathered six scoped
observations but exhausted the durable deadline before summarizing; and
`task_f363c774eb6ddb15d4c1d3a5` completed but mislabeled roughly 6 MiB files
as gigabytes and omitted citations; `task_23ce447ae06a6e022fe72149`
failed before MCP dispatch when DeepSeek shortened `system.get_disk_usage` to
an unlisted tool name. The final target task verifies the
evidence-cited, numerically bounded operational conclusion and Provider
compatibility. The exact malformed-response retry and reserved final-only
branch, as well as the capability-name correction branch, remain `TESTED` by
deterministic provider/runtime tests because the successful final target task
did not need to enter those recovery branches.

## Status decisions and remaining gaps

The audited native evidence promotes all seven Collectors and both in-process adapter models, all 39 MCP read-tool calls/runtime, general provider interaction, safety/approval/executor/rollback, evidence/RCA, hash Trace, port/CPU/disk/file workflows, and Kylin/LoongArch64 compatibility to `TARGET_VERIFIED` for their stated scopes.

Release/deployment is also `TARGET_VERIFIED` for checksum verification, install, start, health, repeated reinstall, default data-preserving uninstall and configuration-continuity restore. The uninstaller intentionally removes `/etc/safeops`; retaining provider and approval-signing identity requires the root-only procedure in `deploy/README.md`.

The target `git` and `go` commands remain absent, although they are not runtime dependencies. External Prometheus/OpenTelemetry deployment and the Registry periodic-health/dependency/version-history loop remain future work and are not implied by the verified in-process adapter run.
