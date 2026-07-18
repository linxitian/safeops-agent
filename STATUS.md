# Project Status

Updated: 2026-07-18

## Milestone status

| Milestone | Status | Evidence |
|---|---:|---|
| M0 Research/spec/matrices | IMPLEMENTED | Required management and seven research documents exist; empty baseline audit recorded |
| M1 Platform/collectors | TARGET_VERIFIED | Installed non-root target run executed all seven normalized Collectors plus Prometheus/OpenTelemetry adapter models with bounded count-only evidence; 7/7 completed without issue or truncation |
| M2 MCP registry/tools | TARGET_VERIFIED | Official Kylin V11/loong64: installed non-root Registry initialized/pinged 8/8 servers and completed one bounded structured call for each of 39/39 tools |
| M3 First vertical slice | TESTED | Ubuntu: live HTTP/SSE run, real MCP `/proc` results, 22-event valid Trace, restart recovery |
| M4 General Agent Runtime | TARGET_VERIFIED | Credentialed compatible-provider runs on official Kylin completed real MCP reads; candidate `384f06c` with DeepSeek V4 Flash completed scoped `/var/log` diagnosis in 21.1 seconds with correct numeric evidence and a `VALID` Trace |
| M5 Guards/risk | TARGET_VERIFIED | Official target injection negative made no tool/approval/execution call; target write flows passed local policy, exact-target risk and independent approvals |
| M6 Executor/approval/rollback | TARGET_VERIFIED | Official target completed fixed process/service/file handlers through the Unix socket, including exact-bound approvals and verified quarantine/restore |
| M7 Evidence/RAG/RCA | TARGET_VERIFIED | Official target port workflow completed D1 correlation and BM25 retrieval with real service/log/socket/process evidence; CPU/disk diagnostic flows also completed |
| M8 Complete trace | TARGET_VERIFIED | Official target exports for provider, port, CPU, disk, injection and file workflows all verified `VALID`; final file flow ended with 23/31/17/19-event chains |
| M9 Port-conflict demo | TARGET_VERIFIED | Official Kylin task `task_eaa766483656e0582748ccfc` completed 10/10 with separate L2/L1 approvals and service/port/HTTP verification; 87-event Trace `VALID` |
| M10 Full durable context/resume | TARGET_VERIFIED | Target state survived repeated installs; Session-selected resources drove later turns and a previously nonterminal slow-provider task recovered durably to `FAILED` with lease release |
| M11 Multi-turn file demo | TARGET_VERIFIED | Merged target release completed discovery, scoped recommendation, exact third-file quarantine and exact-record restore; all four Traces `VALID` and original file identity restored |
| M12 CPU/disk remediation demos | TARGET_VERIFIED | Official target CPU task completed 7/7 and disk/log task 8/8 with fresh snapshots, approvals, post-verification and no physical-space-reclaimed overclaim |
| M13 Full Chinese UI | TARGET_VERIFIED | Installed candidate `2b26de4` (intermediate PR #23 commit; squash-merged as `7479752`) target console passed a real Chrome traversal of all eight views: 39 HTTP 200 responses, no browser/network errors or horizontal overflow, and no unnamed DOM/Accessibility interactive controls |
| M14 Target compatibility | TARGET_VERIFIED | Audited reports identify Kylin V11/loong64, glibc 2.38 and systemd 255; 8/8 MCP discovery/ping and all 39 native tool calls pass, with WARN only for optional target `git`/`go` commands |
| M15 Benchmarks | TARGET_VERIFIED | Installed non-root `safeops-bench all` passed all six suites and measured all 16 metrics natively on official Kylin V11/loong64; controlled-fixture and environment-specific latency caveats remain explicit |
| M16 Release/deploy | TARGET_VERIFIED | Target checksum/install/start/health/reinstall pass; default uninstall removed binaries/config/units while 140 durable file hashes and 153 metadata rows stayed identical, then root-only environment/HMAC restoration preserved continuity |
| M17 Guarded LLM actions/path governance | TESTED | Ubuntu tests enforce explicit operator action verbs, exact structured MCP target identity, approval-bound fixed actions, separated read/write roots, symlink-confined browsing/creation and preservation of the existing Lab write root; official Kylin validation is pending |

## Actual changes in the current development stage

- Installed and SHA-256 verified official Go 1.26.4 in the user directory; module compatibility target is Go 1.25.
- Pinned official MCP Go SDK v1.6.1 and shallow-cloned it into `~/AI-References/go-sdk`.
- Implemented typed Linux CPU, memory, load, disk, mounts, kernel/OS and uptime reads.
- Implemented seven unified Collectors and bounded partial `CollectionBatch`: proc/process, disk/directory/large-file, network, systemd, journal, system configuration/selected sysctl and allowlisted config mtime/size/hash changes; Prometheus/OTel adapter models remain transport-neutral.
- Implemented 8 independent MCP servers with 39 typed, structured L0 tools across system/process/network/journal/service/diagnostic/file/config.
- Added process start-ticks identity, redacted commands, socket-inode/FD port ownership, `/proc/net` socket and interface reads.
- Added fixed, allowlisted, output-bounded `systemctl`/`journalctl` calls with unit injection rejection and log truncation/redaction.
- Added SafeFS allowlist, symlink-escape rejection, bounded metadata/directory/hash/large-file reads and configuration snapshot diff without returning file content.
- Implemented YAML MCP Registry, protocol initialize/discovery, complete Tool/tool-set fingerprints, ping, runtime enable/disable, rediscovery and list-change detection; initialize identity/protocol, lookup-only dependency metadata, non-overlapping periodic checks and bounded failure/recovery histories are now projected through API and Web.
- Implemented atomic Session/Task JSON persistence, bounded CPU/memory orchestrator, progress SSE, and Chinese React console.
- Implemented versioned local Tool Policy, Static Guard, evidence-aware Intent Guard, deterministic contextual Risk and fail-closed Safety Pipeline.
- Integrated Action Proposal + Static/Intent/Risk events into each MCP call; the two-tool slice now records 22 Trace events.
- Implemented durable exact-bound approval records, HMAC ActionEnvelope, nonce replay prevention, executor-side policy/intent/risk/scope/target revalidation and a Unix-socket dry-run executor.
- Implemented deterministic Evidence Graph, D1-D3 RCA confidence formula and pure-Go BM25 knowledge retrieval with source/score/matched-term provenance; connected it to port-conflict diagnosis.
- Added an OpenAI-compatible structured-output Provider and a bounded general Agent loop with discovered-schema validation, exact request-capability name validation, Tool Result re-entry, 12-iteration/30-call limits, replan bounds and no-progress detection; a contract-invalid or evidence-uncited final response receives exactly one bounded correction attempt while transport/HTTP failures are not retried, byte/disk claims are explicitly evidence-constrained, and evidence-backed tasks reserve their final minute for a capability-free final answer.
- Added model requests for only the fixed `service.restart` and `process.terminate` actions; explicit operator intent and exact successful structured MCP identity evidence are checked locally before policy, snapshot and approval preparation.
- Added separate read-only browser and managed-write roots, a graphical directory browser and non-root child-directory creation confined by `os.Root`; deployment retains both `/home` and `/var/lib/safeops/lab` write roots so existing target Demos remain in scope.
- Added bounded, redacted durable Session context for ambiguous general follow-ups and constrained every provider request by the persisted Agent deadline.
- Added approval resolve APIs and automatic Task resume through EXECUTING/VERIFYING, including server-start recovery of resolved approvals.
- Added real allowlisted Lab handlers for atomic file quarantine/restore, reversible delete-by-quarantine, fixed-size file creation, fixed service restart and fixed SIGTERM process termination; permanent purge and arbitrary command execution remain unavailable.
- Added selected-resource persistence, ordinal/pronoun resolution, direct allowlisted file path handling, and tested file create/delete/quarantine/restore flows with post-action context cleanup.
- Added controlled port-holder, demo-Web, CPU-hog and bounded log-writer programs plus hardened Lab units.
- Added fixed Lab-only port recovery, CPU recovery and disk/log-growth Agent state machines. Each persists evidence across approval, binds fresh target snapshots, automatically resumes after separately scoped approvals and verifies explicit completion gates.
- Added Chinese exact-target approval cards with risk level/reasons/factors, reversibility, expiry and target digest; approve/reject confirmation can continue through multiple approvals, and the task view projects recent Trace integrity/evidence.
- Added source-backed Chinese Overview, Tool, Safety, RCA and Audit pages; conversation-first sidebar history, session search/rename/archive/restore; typed SSE IDs with duplicate suppression, bounded replay and durable gap/snapshot recovery.
- Added body-free JSONL runtime access logging under the server data directory for request method/path/status/duration/bytes metadata.
- Added frontend component navigation, unsafe-Markdown escaping and automated serious/critical accessibility checks; static Web responses enforce a strict self-only CSP and related browser security headers.
- Added an explicit bundled SVG favicon and verified the installed target-served Console, Overview, Tool, Safety, RCA, Audit, Allowlist and LLM views through real Chrome without browser/network errors, overflow or unnamed interactive controls.
- Added and audited `targetctl` probe/test/report/doctor on the official Kylin V11/loong64 target. Generated reports correctly remain `target_verified=false`; the separate maintainer audit is recorded in `docs/target-verification-2026-07-18.md`.
- Extended `targetctl test` with a unique, time-bounded official-SDK call for every discovered Tool, dynamic targetctl PID checks, non-secret Lab file/config fixtures, dependency capture, failure aggregation and redacted/bounded error details; successful payloads are not persisted.
- Extended `targetctl test` again with an exact seven-Collector plan, per-collector deadlines, aggregate budgets, count-only Prometheus/OpenTelemetry adapter execution and no persisted Observation values; fixed-file SafeFS snapshots provide deterministic, non-secret configuration evidence without weakening empty-result failure checks.
- Corrected the development and installed `mcp-config` manifests so the comma-separated `/etc/safeops,/var/lib/safeops/lab/config` value remains one argument; regression tests load both manifests and assert both allowlist roots.
- Added `safeops-bench` with six suites, auditable case rows, 16 named metrics, `NOT_MEASURED` for unselected suites and fixed JSON/Markdown reports.
- Added optional prebuilt-Web serving with SPA fallback and static/API isolation tests, so the installed server can deliver the Chinese console without an external Web server.
- Added the M16 release pipeline, absolute installed MCP Manifest, hardened non-root server/root fixed-executor systemd units, checksum-verifying root installer and data-preserving uninstaller.
- Added dual-architecture build script, Makefile, safety instructions and management/research docs.

## Validation results

| Check | Result |
|---|---:|
| `CGO_ENABLED=0 go test ./...` | PASS |
| `CGO_ENABLED=0 go vet ./...` | PASS |
| Frontend TypeScript lint | PASS |
| Frontend production build | PASS |
| linux/amd64 all Go commands | PASS; 16 commands |
| linux/loong64 all Go commands | PASS; 16 commands cross-built |
| MCP in-memory initialize/list/call | PASS; 39 tools across eight domains |
| MCP Registry lifecycle/list-change | PASS; enable, disable, rediscover and stable/change fingerprints |
| MCP Registry automatic health/history | PASS on Ubuntu; official-SDK stdio healthy/failure/recovery, disabled skip, 32-record bounds, actual initialize version/protocol and API/UI projection; target run pending |
| MCP Registry compiled stdio initialize/discovery/ping | PASS; 8 healthy Servers, 39 discovered Tools |
| Official Kylin installed Registry native calls | PASS; unique call plan and structured results for 39/39 Tools as non-root `safeops` |
| SafeFS boundary/hash/config snapshot | PASS; traversal/symlink escape and size bounds tested |
| Unified Collector batches | TARGET_VERIFIED; installed non-root Kylin run completed 7/7 collectors, 195 observations, no issues/truncation and both count-only adapter models; no Observation values or configuration hashes persisted |
| Guard/approval/envelope/executor negatives | PASS; mismatch, injection, tamper, expiry, replay and target change denied |
| Guarded LLM action negatives | PASS; read-only requests, error/unrelated text, mismatched PID/start-ticks and unavailable action names fail closed |
| Path governance boundaries | PASS; read/write roots are separate, parent navigation stays bounded, symlink escapes cannot be browsed or created through, and deployment config preserves `/home` plus the existing Lab root |
| Approval resume and real Lab rollback | PASS; approved/rejected/failure/restart recovery plus real quarantine/restore/create/delete cycles |
| Port recovery state machine | PASS; 10/10 plan, five read tools, D1/RAG, L2 process approval, separate L1 restart approval and HTTP verification |
| CPU recovery state machine | PASS; exact process identity, persisted baseline, post-action process/CPU verification and no false success when recovery is insufficient |
| Disk/log-growth state machine | PASS; fresh post-stop file snapshot, separate L2/L1 approvals, quarantine verification and no physical-space-reclaimed overclaim |
| Durable task concurrency/recovery | PASS; cross-store file locks, exclusive lease/fencing, expiry takeover, stale-write rejection and uncertain external action fail-closed |
| Evidence/RCA/BM25 | PASS; graph stability, confidence components and retrieval provenance |
| `safeops-bench all` | TARGET_VERIFIED; six suites and 16 measured metrics pass on official Kylin V11/loong64 as non-root `safeops`; report stores exact sample counts and methods |
| Final target report checksums | PASS; original probe/test/doctor, full-call `target_ae6d4bbeb9ae7b8e5764`, exact-merge `target_7a42c5e387e7abeea4f8`, Collector follow-up `target_df68d477155fd8d55d75` and exact PR #30 merge regression `target_4a368ebd750533d7ddb6` independently checksum-verified |
| Official Kylin V11 native checks | PASS with bounded WARN; loong64/Kylin/glibc/systemd/proc/statfs, 8/8 MCP, 39/39 native Tool calls, 7/7 Collectors and both adapter models pass; only optional `git`/`go` commands absent |
| Credentialed compatible-provider runs | TARGET_VERIFIED; DeepSeek V4 Flash target task `task_29ebd42581d2dda9a4670aca` completed in 21.1 seconds with two scoped reads, two exact evidence citations and a `VALID` Trace |
| Structured-decision recovery | TESTED; strict decoding remains authoritative, one malformed-response correction is bounded to a single retry, repeated invalid output fails, and provider/HTTP failures are not retried |
| Deadline-aware finalization | TESTED; no final-only mode is entered before evidence, the reserved final request retains observations but exposes no tools/actions, and non-final output fails closed |
| Evidence-cited operational conclusions | TARGET_VERIFIED; the final Kylin task reported 13.86% use, made no disk-pressure/MB-to-GB overclaim, disclosed the permission-limited large-file read and cited both exact Trace references; repair negatives remain unit-tested |
| Provider capability-name drift | TESTED; shortened, invented or mismatched Server/Tool/managed-action names receive one bounded correction and are never silently guessed or dispatched |
| Installed port/CPU/disk/file workflows | PASS; 10/10, 7/7, 8/8 and final four-turn quarantine/restore flows completed with `VALID` Traces |
| M16 release pipeline | PASS locally; tests/vet/frontend plus 16 amd64 and 16 loong64 commands, fixed tar.gz and outer SHA256 produced |
| Release artifact verification | PASS; outer hash, 39 bundle-file hashes, 16 static LoongArch ELF files and six staged systemd units verified |
| Target install/start/health/reinstall | PASS; final release checksum verified, services healthy, operator LLM environment preserved |
| Target data-preserving uninstall | PASS; `/opt`/`/etc`/six units removed, `/var/lib/safeops` hashes and metadata identical, user/group retained, root-only configuration continuity restored before reinstall |
| Real Ubuntu `/proc` Platform smoke | PASS |
| Live Session → Task → MCP → answer → SSE | PASS |
| Restart restores completed Session/Task/Trace | PASS |
| Complete Trace audit | PASS; 48 concurrent appends, exact lifecycle events, DecisionRecord fields, redaction, crash full/partial tail recovery and modification/delete/reorder rejection |
| Typed SSE recovery | PASS; monotonic IDs, recent replay, duplicate suppression, truncation/restart gap plus durable Task/Trace resync |
| Chinese Web component/accessibility | PASS; all eight views, conversation-first sidebar history, source-backed projections, unsafe Markdown escaping and no serious/critical automated violations |
| Installed target Web real-browser audit | PASS; eight views, 39/39 HTTP 200 responses, zero console/log/exception/loading failures, zero horizontal overflow and zero unnamed DOM/Chrome AX interactive controls |
| Official Kylin VM runtime | TARGET_VERIFIED; audited native reports and installed workflow evidence on release `1a10880` |
| `make` entry points on current host | NOT_RUN; `make` is absent, exact equivalent commands passed |
| Race detector | NOT_RUN; conflicts with default `CGO_ENABLED=0` and was not used as evidence |

## Live vertical-slice evidence

Request: `查看 CPU 和内存。`

- Registry state: 8 Servers `HEALTHY`, 39 discovered tools.
- Task plan: `system.get_cpu_metrics` then `system.get_memory_metrics`.
- Latest sample: CPU 1.5% over a 200 ms `/proc/stat` interval; memory 31.5%, 4.84 GiB used of 15.35 GiB (sample values vary by run).
- Task: `COMPLETED`, two completed steps, two Trace evidence references.
- Trace: 22 events, including two Action Proposal/Static Guard/Intent Guard/Risk groups plus plan/tool/result/final; integrity `VALID`.
- SSE: understanding, collecting, CPU evidence, memory evidence, completed.
- After process restart: one Session, two Messages, completed Task and valid Trace restored from disk.

## Competition matrix change

- MCP registry/tools, the bounded general Agent, Guards/risk, fixed executor/approval/rollback, Evidence/RAG/RCA, complete Trace and durable context/resume are now `TARGET_VERIFIED` for their audited target paths.
- Every one of the 39 MCP read Tools now has a successful official Kylin native structured-call check; no Tool remains discovery-only in the audited matrix.
- Port, CPU, disk/log and multi-turn file Demos are now `TARGET_VERIFIED`; exact task and Trace identifiers are in `docs/target-verification-2026-07-18.md`.
- Kylin V11/LoongArch64 compatibility and release/deployment are `TARGET_VERIFIED`; the documented root-only backup is required only when operator configuration and approval-signing identity must survive the intentional `/etc` removal.
- OS depth perception is now `TARGET_VERIFIED`: all seven Collectors and both transport-neutral adapter models executed in the installed non-root target runtime with bounded count-only evidence. This does not claim deployment of an external telemetry platform.
- Benchmark/evaluation is now `TARGET_VERIFIED` for controlled native execution: all six installed suites and 16 metrics passed on Kylin/loong64; no fixture metric is presented as a real-world accuracy estimate.

## Currently complete demo scenarios

The original Ubuntu CPU/memory Web vertical slice remains complete. The official Kylin V11/loong64 installation now also has evidence-backed live API/UI paths for:

- a credentialed compatible-provider read using real MCP evidence;
- port-conflict diagnosis and recovery, 10/10 with separate process and service approvals;
- exact CPU-hog recovery, 7/7 with pre/post CPU and process verification;
- bounded disk/log-growth recovery, 8/8 with separate process/file approvals and honest quarantine semantics;
- a four-turn file conversation that discovers three files, keeps a follow-up within those resources, quarantines the third and restores the exact record.

All named target workflows have `VALID` hash-chained Traces. The eight-view UI is `TARGET_VERIFIED` by a systematic real-Chrome traversal of the installed target assets and APIs with DOM and Chrome Accessibility Tree capture.

## Real blockers

- The target image lacks optional `git` and `go` commands. They are not release runtime dependencies, but their absence remains visible as bounded report warnings.
- `make` is absent on the Ubuntu development host, but equivalent direct commands pass, so this does not block development.
- Registry periodic health scheduling, dependency-state projection, actual initialize identity and bounded history are `TESTED` on Ubuntu; the exact candidate still needs installed official-Kylin evidence before this new scope is `TARGET_VERIFIED`.
- M17's new model-proposed fixed-action and path-browser scope still needs an exact merged release run on the official Kylin VM before it can be called `TARGET_VERIFIED`.

## Human action required now

The immediate task is the exact candidate release and official-Kylin verification of automatic Registry refresh, 8/8 dependency health, actual versions and the existing 39/39 target call report. After that, the remaining feature-level target gap is M17's guarded general-action/path-browser flow.
