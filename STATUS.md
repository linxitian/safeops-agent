# Project Status

Updated: 2026-07-18

## Milestone status

| Milestone | Status | Evidence |
|---|---:|---|
| M0 Research/spec/matrices | IMPLEMENTED | Required management and seven research documents exist; empty baseline audit recorded |
| M1 Platform/collectors | TESTED | Seven normalized Collectors cover proc/process, disk/directory/large-file, network, systemd, journal, system config/sysctl and allowlisted config changes; bounded partial batches, adapters, fixtures and real Linux smoke pass |
| M2 MCP registry/tools | TARGET_VERIFIED | Official Kylin V11/loong64: native Registry initialized and pinged 8/8 servers, discovered 39/39 tools and returned a structured real `/proc` memory result |
| M3 First vertical slice | TESTED | Ubuntu: live HTTP/SSE run, real MCP `/proc` results, 22-event valid Trace, restart recovery |
| M4 General Agent Runtime | TARGET_VERIFIED | Credentialed compatible-provider runs on official Kylin completed real MCP reads; merged release `1a10880` preserved bounded durable follow-up scope and enforced the two-minute Agent deadline |
| M5 Guards/risk | TARGET_VERIFIED | Official target injection negative made no tool/approval/execution call; target write flows passed local policy, exact-target risk and independent approvals |
| M6 Executor/approval/rollback | TARGET_VERIFIED | Official target completed fixed process/service/file handlers through the Unix socket, including exact-bound approvals and verified quarantine/restore |
| M7 Evidence/RAG/RCA | TARGET_VERIFIED | Official target port workflow completed D1 correlation and BM25 retrieval with real service/log/socket/process evidence; CPU/disk diagnostic flows also completed |
| M8 Complete trace | TARGET_VERIFIED | Official target exports for provider, port, CPU, disk, injection and file workflows all verified `VALID`; final file flow ended with 23/31/17/19-event chains |
| M9 Port-conflict demo | TARGET_VERIFIED | Official Kylin task `task_eaa766483656e0582748ccfc` completed 10/10 with separate L2/L1 approvals and service/port/HTTP verification; 87-event Trace `VALID` |
| M10 Full durable context/resume | TARGET_VERIFIED | Target state survived repeated installs; Session-selected resources drove later turns and a previously nonterminal slow-provider task recovered durably to `FAILED` with lease release |
| M11 Multi-turn file demo | TARGET_VERIFIED | Merged target release completed discovery, scoped recommendation, exact third-file quarantine and exact-record restore; all four Traces `VALID` and original file identity restored |
| M12 CPU/disk remediation demos | TARGET_VERIFIED | Official target CPU task completed 7/7 and disk/log task 8/8 with fresh snapshots, approvals, post-verification and no physical-space-reclaimed overclaim |
| M13 Full Chinese UI | TESTED | Six Chinese pages, conversation-first sidebar history, search/rename/archive, approval/result cards, RCA/audit projections, typed SSE replay/gap/snapshot sync, strict CSP, component navigation/unsafe-Markdown and serious accessibility checks pass |
| M14 Target compatibility | TARGET_VERIFIED | Audited final-release reports identify Kylin V11/loong64, glibc 2.38 and systemd 255; 8/8 MCP and 39/39 tools pass, with WARN only for optional target `git`/`go` commands |
| M15 Benchmarks | TESTED | Six `safeops-bench` suites, 16 measured metrics, fixed JSON/Markdown artifacts and full milestone gates pass on Ubuntu |
| M16 Release/deploy | PARTIAL | Final `1a10880` archive hash, target install/start/health and repeated reinstall/environment preservation pass; data-preserving target uninstall remains untested |

## Actual changes in the current development stage

- Installed and SHA-256 verified official Go 1.26.4 in the user directory; module compatibility target is Go 1.25.
- Pinned official MCP Go SDK v1.6.1 and shallow-cloned it into `~/AI-References/go-sdk`.
- Implemented typed Linux CPU, memory, load, disk, mounts, kernel/OS and uptime reads.
- Implemented seven unified Collectors and bounded partial `CollectionBatch`: proc/process, disk/directory/large-file, network, systemd, journal, system configuration/selected sysctl and allowlisted config mtime/size/hash changes; Prometheus/OTel adapter models remain transport-neutral.
- Implemented 8 independent MCP servers with 39 typed, structured L0 tools across system/process/network/journal/service/diagnostic/file/config.
- Added process start-ticks identity, redacted commands, socket-inode/FD port ownership, `/proc/net` socket and interface reads.
- Added fixed, allowlisted, output-bounded `systemctl`/`journalctl` calls with unit injection rejection and log truncation/redaction.
- Added SafeFS allowlist, symlink-escape rejection, bounded metadata/directory/hash/large-file reads and configuration snapshot diff without returning file content.
- Implemented YAML MCP Registry, protocol initialize/discovery, complete Tool/tool-set fingerprints, ping, runtime enable/disable, rediscovery and list-change detection.
- Implemented atomic Session/Task JSON persistence, bounded CPU/memory orchestrator, progress SSE, and Chinese React console.
- Implemented versioned local Tool Policy, Static Guard, evidence-aware Intent Guard, deterministic contextual Risk and fail-closed Safety Pipeline.
- Integrated Action Proposal + Static/Intent/Risk events into each MCP call; the two-tool slice now records 22 Trace events.
- Implemented durable exact-bound approval records, HMAC ActionEnvelope, nonce replay prevention, executor-side policy/intent/risk/scope/target revalidation and a Unix-socket dry-run executor.
- Implemented deterministic Evidence Graph, D1-D3 RCA confidence formula and pure-Go BM25 knowledge retrieval with source/score/matched-term provenance; connected it to port-conflict diagnosis.
- Added an OpenAI-compatible structured-output Provider and a bounded general Agent loop with discovered-schema validation, Tool Result re-entry, 12-iteration/30-call limits, replan bounds and no-progress detection.
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
- Added and audited `targetctl` probe/test/report/doctor on the official Kylin V11/loong64 target. Generated reports correctly remain `target_verified=false`; the separate maintainer audit is recorded in `docs/target-verification-2026-07-18.md`.
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
| MCP Registry compiled stdio initialize/discovery/ping | PASS; 8 healthy Servers, 39 discovered Tools |
| SafeFS boundary/hash/config snapshot | PASS; traversal/symlink escape and size bounds tested |
| Unified Collector batches | PASS; all seven required collectors, partial permission failure, timeout/output budgets, no config-body persistence, adapters and real Linux smoke |
| Guard/approval/envelope/executor negatives | PASS; mismatch, injection, tamper, expiry, replay and target change denied |
| Approval resume and real Lab rollback | PASS; approved/rejected/failure/restart recovery plus real quarantine/restore/create/delete cycles |
| Port recovery state machine | PASS; 10/10 plan, five read tools, D1/RAG, L2 process approval, separate L1 restart approval and HTTP verification |
| CPU recovery state machine | PASS; exact process identity, persisted baseline, post-action process/CPU verification and no false success when recovery is insufficient |
| Disk/log-growth state machine | PASS; fresh post-stop file snapshot, separate L2/L1 approvals, quarantine verification and no physical-space-reclaimed overclaim |
| Durable task concurrency/recovery | PASS; cross-store file locks, exclusive lease/fencing, expiry takeover, stale-write rejection and uncertain external action fail-closed |
| Evidence/RCA/BM25 | PASS; graph stability, confidence components and retrieval provenance |
| `safeops-bench all` | PASS; six suites and 16 measured metrics; report stores exact sample counts and methods |
| Final target report checksums | PASS; probe `target_0f70b837d6ab5d7c72bd`, test `target_3ca1769f7eb00323a1d8`, doctor `target_d05c9711293c083a26c2` |
| Official Kylin V11 native checks | PASS with bounded WARN; loong64/Kylin/glibc/systemd/proc/statfs and 8/8 MCP, 39/39 tools pass; only optional `git`/`go` commands absent |
| Credentialed compatible-provider runs | PASS; real MCP evidence, final three-resource follow-up scope and provider-deadline failure persistence verified |
| Installed port/CPU/disk/file workflows | PASS; 10/10, 7/7, 8/8 and final four-turn quarantine/restore flows completed with `VALID` Traces |
| M16 release pipeline | PASS locally; tests/vet/frontend plus 16 amd64 and 16 loong64 commands, fixed tar.gz and outer SHA256 produced |
| Release artifact verification | PASS; outer hash, 39 bundle-file hashes, 16 static LoongArch ELF files and six staged systemd units verified |
| Target install/start/health/reinstall | PASS; final release checksum verified, services healthy, operator LLM environment preserved |
| Target data-preserving uninstall | NOT_TESTED; requires a disposable VM snapshot so installed configuration can be safely removed |
| Real Ubuntu `/proc` Platform smoke | PASS |
| Live Session → Task → MCP → answer → SSE | PASS |
| Restart restores completed Session/Task/Trace | PASS |
| Complete Trace audit | PASS; 48 concurrent appends, exact lifecycle events, DecisionRecord fields, redaction, crash full/partial tail recovery and modification/delete/reorder rejection |
| Typed SSE recovery | PASS; monotonic IDs, recent replay, duplicate suppression, truncation/restart gap plus durable Task/Trace resync |
| Chinese Web component/accessibility | PASS; all six pages, conversation-first sidebar history, source-backed projections, unsafe Markdown escaping and no serious/critical automated violations |
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
- Port, CPU, disk/log and multi-turn file Demos are now `TARGET_VERIFIED`; exact task and Trace identifiers are in `docs/target-verification-2026-07-18.md`.
- Kylin V11/LoongArch64 compatibility is `TARGET_VERIFIED`. Release/deployment remains `PARTIAL` because uninstall is not yet target-tested.
- OS depth perception and the six-page Chinese Web remain `TESTED`: native workflows cover their used paths, but not every Collector adapter or a systematic target-browser traversal.
- Benchmark/evaluation remains `TESTED` on controlled development-host suites and is not promoted using unrelated target evidence.

## Currently complete demo scenarios

The original Ubuntu CPU/memory Web vertical slice remains complete. The official Kylin V11/loong64 installation now also has evidence-backed live API/UI paths for:

- a credentialed compatible-provider read using real MCP evidence;
- port-conflict diagnosis and recovery, 10/10 with separate process and service approvals;
- exact CPU-hog recovery, 7/7 with pre/post CPU and process verification;
- bounded disk/log-growth recovery, 8/8 with separate process/file approvals and honest quarantine semantics;
- a four-turn file conversation that discovers three files, keeps a follow-up within those resources, quarantines the third and restores the exact record.

All named target workflows have `VALID` hash-chained Traces. The six-page UI remains `TESTED`, not `TARGET_VERIFIED`, because a systematic target-browser traversal/accessibility capture was not performed.

## Real blockers

- Data-preserving uninstall remains unverified on the target; it should be exercised only after taking a disposable VM snapshot because `/etc/safeops` is intentionally removed.
- A systematic target-browser traversal of all six pages and target-side accessibility capture remains outstanding.
- The target image lacks optional `git` and `go` commands. They are not release runtime dependencies, but their absence remains visible as bounded report warnings.
- `make` is absent on the Ubuntu development host, but equivalent direct commands pass, so this does not block development.

## Human action required now

Before testing uninstall, take a disposable snapshot of the target VM. The next manual evidence priorities are the data-preserving uninstall/reinstall cycle and a recorded traversal of all six installed Web pages; provider credentials and official target reports are no longer blockers.
