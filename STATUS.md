# Project Status

Updated: 2026-07-16

## Milestone status

| Milestone | Status | Evidence |
|---|---:|---|
| M0 Research/spec/matrices | IMPLEMENTED | Required management and seven research documents exist; empty baseline audit recorded |
| M1 Platform/collectors | TESTED | Seven normalized Collectors cover proc/process, disk/directory/large-file, network, systemd, journal, system config/sysctl and allowlisted config changes; bounded partial batches, adapters, fixtures and real Linux smoke pass |
| M2 MCP registry/tools | TESTED | Ubuntu: official SDK v1.6.1, YAML Registry, 8/8 servers and 39 tools; lifecycle, list-change, protocol and live discovery pass |
| M3 First vertical slice | TESTED | Ubuntu: live HTTP/SSE run, real MCP `/proc` results, 22-event valid Trace, restart recovery |
| M4 General Agent Runtime | PARTIAL | OpenAI-compatible structured provider and bounded ReAct/replan runtime are tested with contract fakes; no credentialed provider run |
| M5 Guards/risk | TESTED | Local policy, Static/Intent Guard, contextual L0-L3 risk and injection/mismatch/critical-target cases pass |
| M6 Executor/approval/rollback | TESTED | Signed envelopes, durable approvals, auto-resume, replay/scope/target checks and real Lab quarantine/restore plus fixed service/process handlers pass Ubuntu tests |
| M7 Evidence/RAG/RCA | TESTED | Deterministic graph, BM25 provenance and port/high-CPU/disk D1-D3 diagnostic protocol tests pass; required end-user demos remain partial |
| M8 Complete trace | TESTED | Exact lifecycle events, complete DecisionRecord, recursive secret/CoT redaction, concurrent append, crash-tail reconciliation and tamper/delete/reorder rejection pass |
| M9 Port-conflict demo | PARTIAL | Tested 10-step backend state machine performs five real read tools, D1/RAG, separately approved process termination/service restart, and service/port/HTTP verification; installed live-system run remains absent |
| M10 Full durable context/resume | TESTED | Cross-process locks, atomic Session mutation, Task lease/fencing, checkpoints, safe startup resume, uncertain-write fail-closed recovery and session search/rename/archive pass |
| M11 Multi-turn file demo | PARTIAL | Selected-resource capture, ordinal resolution, approval-bound quarantine/restore and context cleanup pass backend tests; full live chat/UI demo pending |
| M12 CPU/disk remediation demos | PARTIAL | Tested backend state machines bind exact process/file targets, preserve pre/post evidence, reject false CPU recovery, and honestly distinguish quarantine from physical disk reclamation; installed live-system runs remain absent |
| M13 Full Chinese UI | TESTED | Six Chinese pages, search/rename/archive, approval/result cards, RCA/audit projections, typed SSE replay/gap/snapshot sync, strict CSP, component navigation/unsafe-Markdown and serious accessibility checks pass |
| M14 Target compatibility | PARTIAL | `targetctl` probe/test/report/doctor and fixed reports pass locally; linux/loong64 build passes; official VM untested |
| M15 Benchmarks | TESTED | Six `safeops-bench` suites, 16 measured metrics, fixed JSON/Markdown artifacts and full milestone gates pass on Ubuntu |
| M16 Release/deploy | PARTIAL | Full release gate created the required LoongArch64 tar/SHA256, verified 39 bundle files and six staged systemd units; target root install/start/health/uninstall remain untested |

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
- Added approval resolve APIs and automatic Task resume through EXECUTING/VERIFYING, including server-start recovery of resolved approvals.
- Added real allowlisted Lab handlers for atomic file quarantine/restore, fixed service restart and fixed SIGTERM process termination; permanent purge and arbitrary command execution remain unavailable.
- Added selected-resource persistence, ordinal/pronoun resolution and a tested two-turn quarantine then restore flow with post-action context cleanup.
- Added controlled port-holder, demo-Web, CPU-hog and bounded log-writer programs plus hardened Lab units.
- Added fixed Lab-only port recovery, CPU recovery and disk/log-growth Agent state machines. Each persists evidence across approval, binds fresh target snapshots, automatically resumes after separately scoped approvals and verifies explicit completion gates.
- Added Chinese exact-target approval cards with risk level/reasons/factors, reversibility, expiry and target digest; approve/reject confirmation can continue through multiple approvals, and the task view projects recent Trace integrity/evidence.
- Added source-backed Chinese Overview, Tool, Safety, RCA and Audit pages; session search/rename/archive/restore; typed SSE IDs with duplicate suppression, bounded replay and durable gap/snapshot recovery.
- Added frontend component navigation, unsafe-Markdown escaping and automated serious/critical accessibility checks; static Web responses enforce a strict self-only CSP and related browser security headers.
- Added `targetctl` read-only probe/test/report/doctor with fixed probe/test JSON and text reports; local evidence remains `target_verified=false`.
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
| Approval resume and real Lab rollback | PASS; approved/rejected/failure/restart recovery plus three real quarantine/restore cycles |
| Port recovery state machine | PASS; 10/10 plan, five read tools, D1/RAG, L2 process approval, separate L1 restart approval and HTTP verification |
| CPU recovery state machine | PASS; exact process identity, persisted baseline, post-action process/CPU verification and no false success when recovery is insufficient |
| Disk/log-growth state machine | PASS; fresh post-stop file snapshot, separate L2/L1 approvals, quarantine verification and no physical-space-reclaimed overclaim |
| Durable task concurrency/recovery | PASS; cross-store file locks, exclusive lease/fencing, expiry takeover, stale-write rejection and uncertain external action fail-closed |
| Evidence/RCA/BM25 | PASS; graph stability, confidence components and retrieval provenance |
| `safeops-bench all` | PASS; six suites and 16 measured metrics; report stores exact sample counts and methods |
| `targetctl probe/test` on development host | WARN; Ubuntu/amd64, 8/8 MCP and 39/39 tools pass, `target_verified=false` |
| M16 release pipeline | PASS locally; tests/vet/frontend plus 16 amd64 and 16 loong64 commands, fixed tar.gz and outer SHA256 produced |
| Release artifact verification | PASS; outer hash, 39 bundle-file hashes, 16 static LoongArch ELF files and six staged systemd units verified |
| Target install/start/health/uninstall | NOT_TESTED; installer intentionally rejects this Ubuntu/amd64 host |
| Real Ubuntu `/proc` Platform smoke | PASS |
| Live Session → Task → MCP → answer → SSE | PASS |
| Restart restores completed Session/Task/Trace | PASS |
| Complete Trace audit | PASS; 48 concurrent appends, exact lifecycle events, DecisionRecord fields, redaction, crash full/partial tail recovery and modification/delete/reorder rejection |
| Typed SSE recovery | PASS; monotonic IDs, recent replay, duplicate suppression, truncation/restart gap plus durable Task/Trace resync |
| Chinese Web component/accessibility | PASS; all six pages, source-backed projections, unsafe Markdown escaping and no serious/critical automated violations |
| Official Kylin VM runtime | NOT_TESTED |
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

- Safety intent validation and contextual risk: `PARTIAL` → `TESTED`; safety benchmark covers dangerous, benign, injection and five unauthorized-execution cases.
- OS depth perception: `PARTIAL` → `TESTED`; seven normalized collectors now have bounded partial batches, fixtures/adapters and real Linux read-only smoke.
- Least-privilege execution, approval and rollback: `PARTIAL` → `TESTED` for Ubuntu backend boundaries and scoped Lab operations.
- Durable Session/Task and approval resume: `PARTIAL` → `TESTED`, including reconstructed-store restart recovery.
- Evidence/RAG/RCA: `PARTIAL` → `TESTED` for deterministic modules and diagnostic MCP protocol behavior; full primary Demo remains `PARTIAL`.
- Reasoning-chain traceability: `PARTIAL` → `TESTED` for the complete local lifecycle, redaction, concurrency, crash recovery and audit projection.
- Chinese Web UI: `PARTIAL` → `TESTED` for six pages, session lifecycle, approval/result cards, resilient SSE and component/accessibility checks; installed target Demos remain separate `PARTIAL` rows.
- Port, CPU and disk/log Agent backends now have tested bounded recovery state machines; their matrix rows remain `PARTIAL` until controlled services are installed and the real live/API/UI paths are exercised.
- Multi-turn file flow remains `PARTIAL`: the backend ordinal/quarantine/restore chain and approval card are tested/built, but the live UI Demo is not complete.
- Target tooling: `NOT_STARTED` → `PARTIAL`; local native reports and cross-build exist, but official Kylin remains `NOT_TESTED`.
- Benchmark/evaluation: `NOT_STARTED` → `TESTED`; all six suites and 16 metrics have current local reports.
- Release/deployment: `NOT_STARTED` → `PARTIAL`; release and staged-unit evidence pass locally, while target root install/start/health/uninstall remain untested.

## Currently complete demo scenarios

One end-user scenario is complete on Ubuntu: create/load a Chinese Web Session, ask for CPU and memory, observe a two-step real MCP task and SSE progress, receive a Chinese answer, refresh/restart, and inspect a valid hash-chained Trace.

Controlled Lab components can reproduce a real loopback port collision, bounded CPU pressure and bounded log growth. Automated backend tests now run the bounded port, CPU and disk remediation plans across durable approvals and completion gates, and the multi-turn file test performs approval-bound quarantine and restoration. These are tested backend scenarios, not yet complete end-user competition Demos: the controlled units have not been installed and exercised through the live API/UI on the target.

## Real blockers

- General LLM-provider behavior cannot receive real-provider validation until `SAFEOPS_LLM_BASE_URL`, `SAFEOPS_LLM_API_KEY`, and `SAFEOPS_LLM_MODEL` are supplied; implementation can proceed with contract fakes first.
- Official Kylin runtime/compatibility and final Demo verification require the user to run the bundled VM checklist and return its native reports/evidence.
- `make` is absent on this Ubuntu host, but equivalent direct commands are available, so this does not block development.

## Human action required now

Execute the single official-VM workflow in `deploy/README.md` using the final release, follow the bounded `lab/README.md` scenarios, and return the target reports plus Task/Trace evidence. Supply OpenAI-compatible credentials in that VM only if the multi-turn Provider Demo is to be validated.
