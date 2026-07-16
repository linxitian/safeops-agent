# SafeOps Delivery Plan

Updated: 2026-07-16. Status is evidence-based; completion evidence lives in `STATUS.md` and the two matrices.

## M0 — Reference research, specification, and matrices

- **Scope:** Audit baseline, research official/reference architectures, establish safety invariants, specification, ADRs, status and coverage matrices.
- **Files:** `docs/research/*`, `docs/architecture-gap-v3.md`, `SPEC.md`, `PLAN.md`, `ADR.md`, `STATUS.md`, both matrices.
- **Dependencies:** Source/network access.
- **Acceptance:** Required research topics and all management documents exist; every claim distinguishes tested, planned, and target-verified state.
- **Validation:** Link/source review; matrix-to-worktree audit; `rg 'TARGET_VERIFIED|TESTED'` and inspect every claimed evidence link.
- **Mapping:** All competition requirements; especially MCP, durable agent, policy/trace, Linux perception and B/S interaction.

## M1 — Platform, collectors, and OS perception

- **Scope:** Typed Linux platform; Procfs, Disk, Network, Systemd, Journal, SystemConfig and allowlisted ConfigChange collectors; normalized Observation batches and adapter interfaces.
- **Files:** `internal/platform`, `internal/perception`, collector fixtures/tests.
- **Dependencies:** M0 contracts.
- **Acceptance:** Real CPU/memory/load/process/filesystem/network/service/journal/config state; bounded partial collection; no scattered OS execution.
- **Validation:** `go test ./internal/platform/... ./internal/perception/...`; real read-only Linux smoke; malformed/permission/race/timeout fixtures.
- **Mapping:** OS depth perception, multi-source correlation, platform portability.

## M2 — MCP registry and baseline MCP tools

- **Scope:** Official SDK; manifest registry, lifecycle, health, discovery, schema versioning; eight MCP server domains and at least 20 meaningful tools.
- **Files:** `cmd/mcp-*`, `internal/mcpservers`, `internal/registry`, `config/mcp_servers.yaml`, MCP matrix.
- **Dependencies:** M1 typed platform.
- **Acceptance:** initialize/tools-list/tools-call over protocol; enable/disable/rediscover/health/list-change; unknown writes fail closed in local policy classification.
- **Validation:** in-memory protocol tests, compiled stdio subprocess tests, schema golden/fingerprint changes, Inspector or conformance equivalent.
- **Mapping:** MCP operations plug-ins, extensibility, tool center, risk metadata.

## M3 — First real perception vertical slice

- **Scope:** Chinese CPU/memory request through persistent Session/Task, bounded Agent plan, Registry, `mcp-system`, `/proc`, result observation, Trace and SSE.
- **Files:** `cmd/safeops-server`, `internal/agent`, `internal/api`, `internal/storage`, `internal/trace`, `web`.
- **Dependencies:** M1 platform and M2 system MCP.
- **Acceptance:** refresh and server restart retain messages/task; two MCP tools are called; completion gate produces Chinese answer; SSE shows progress.
- **Validation:** live HTTP/SSE script, restart recovery, protocol tests, frontend build.
- **Mapping:** Natural language interaction, real Linux data, MCP, persistent B/S, trace.

## M4 — General Agent Runtime and ReAct / Plan-Execute

- **Scope:** OpenAI-compatible provider, intent/task classification, general state reducer, context builder, completion gates, loop/no-progress/replan controls.
- **Files:** `internal/llm`, `internal/intent`, `internal/context`, expanded `internal/agent` and task checkpoints.
- **Dependencies:** M2 capabilities and M3 persistence baseline.
- **Acceptance:** every Tool Result re-enters runtime; 12/30/timeout budgets persist across resume; no hidden chain-of-thought stored.
- **Validation:** recorded tool-result sequences, loop/no-progress tests, provider contract tests with a fake HTTP server; real provider test only when credentials supplied.
- **Mapping:** Multi-step reasoning, continuity, auditable decisions.

## M5 — Static Guard, Intent Guard, and contextual risk

- **Scope:** Versioned local policy, action proposal, deterministic target alignment and L0-L3 contextual score/factors.
- **Files:** `internal/guard`, `internal/intent`, `policies`, `contracts/action_proposal*`.
- **Dependencies:** M4 structured objective/plan/action.
- **Acceptance:** deny > approval > allow; unknown write denied; critical targets/scope/batch/non-reversibility raise risk; injection cannot bypass.
- **Validation:** table/fuzz tests for safe, dangerous, ambiguous and injected requests; policy error/undefined fail closed.
- **Mapping:** Safety intent validator, risk classification, prompt-injection resistance.

## M6 — Executor, approval, rollback, and restricted execution

- **Scope:** Durable approval; auto-resume; ActionEnvelope; Unix-socket privileged executor with fixed handlers; target revalidation, replay protection, verification and rollback manager.
- **Files:** `cmd/safeops-privexec`, `internal/executor`, `internal/approval`, `internal/rollback`, `contracts`, deploy hardening.
- **Dependencies:** M5 decisions and M4 durable checkpoints.
- **Acceptance:** no generic command handler; server/MCP non-root; changed targets and replay denied; L1 rollback explicit; approval resumes automatically.
- **Validation:** socket authentication/permissions, envelope tamper/replay/expiry/PID reuse/file change, crash-window reconciliation, approval idempotency.
- **Mapping:** Least privilege, approval, restricted execution, verification and rollback.

## M7 — Evidence Graph, retrieval, and D1-D3 RCA

- **Scope:** Evidence nodes/relations, hypotheses and negative evidence, deterministic confidence, pure-Go lexical retrieval, reviewed case candidates.
- **Files:** `internal/evidence`, `internal/retrieval`, `internal/rca`, `knowledge`.
- **Dependencies:** M1 observations, M4 decisions, M6 verification evidence.
- **Acceptance:** root causes cite evidence; confidence equals tested component formula; inconclusive evidence becomes D3; retrieved docs include source/score/terms.
- **Validation:** recorded RCA fixtures, contradictions, missing permissions/data, multi-root cause and confidence unit tests.
- **Mapping:** RCA, multi-source evidence, RAG, explainability.

## M8 — Complete reasoning and hash audit trace

- **Scope:** All required event types, structured DecisionRecord, redaction, canonical JSON, durable replay, integrity tooling and audit UI API.
- **Files:** `internal/trace`, API endpoints, audit tests.
- **Dependencies:** M4-M7 event producers.
- **Acceptance:** modification/deletion/reorder/hash mismatch detected; every proposal/guard/approval/execution/verify/rollback linked; no secret/raw CoT.
- **Validation:** corruption/crash-tail/concurrent append/restart tests and event coverage assertions.
- **Mapping:** Reasoning-chain traceability, security audit and replay.

## M9 — Port-conflict complete demo

- **Scope:** Controlled web service/port holder, evidence correlation, D1/D2 RCA, L2 approval, terminate/restart, HTTP verify.
- **Files:** `lab/port-conflict`, relevant MCP tools, demo runner/tests.
- **Dependencies:** M1-M8 complete loop.
- **Acceptance:** one Chinese request completes automatically except approval; revalidates PID/service; verifies service and port/HTTP.
- **Validation:** repeated lab setup/run/teardown, approval rejection/expiry/target-change and restart recovery tests.
- **Mapping:** Primary competition demo and all five pillars.

## M10 — Full Session, Context, Durable Task, and approval resume

- **Scope:** Message/checkpoint stores, rolling/working/active/pinned context, references, leases/fencing, safe restart recovery and automatic approval consumer.
- **Files:** `internal/session`, `internal/context`, `internal/task`, `internal/storage`, `internal/approval`.
- **Dependencies:** M4 runtime and M6 action ledger.
- **Acceptance:** history/rename/archive/resume; “continue/third/that service” resolves from structured context; budgets survive restart; no duplicate worker/action.
- **Validation:** store conformance, crash injection at each boundary, split-brain lease and restart continuity tests.
- **Mapping:** Persistent sessions, multi-turn context, HITL continuity.

## M11 — Multi-turn file quarantine demo

- **Scope:** Lab large-file inspection, recommendations/selected resources, reference resolution, quarantine approval, verification and third-item restore.
- **Files:** `cmd/mcp-file`, file platform/tools, `lab`, context/UI tests.
- **Dependencies:** M6 and M10.
- **Acceptance:** all paths remain allowlisted; inode/mtime/size revalidated; quarantine is reversible; permanent purge denied by default.
- **Validation:** exact dialog e2e, ordering/reference tests, symlink/path traversal/TOCTOU and rollback tests.
- **Mapping:** Context memory, approval resume, real reversible execution.

## M12 — CPU and bounded disk demos

- **Scope:** Controlled CPU hog and `/var/lib/safeops/lab` log growth, diagnosis, guarded remediation and recovery verification.
- **Files:** `lab/cpu-hog`, `lab/disk-pressure`, diagnostic tools and tests.
- **Dependencies:** M7 RCA and M6 executor.
- **Acceptance:** stable repeated demos; never fill root filesystem; injection asking `rm -rf` is denied.
- **Validation:** resource caps, setup/teardown repetition, metric-before/after evidence and safety negative tests.
- **Mapping:** OS perception, RCA, safe remediation and two required demos.

## M13 — Chinese Web UI completion

- **Scope:** Console, overview, tool center, safety center, RCA and audit pages; approval/result cards; replayable typed SSE.
- **Files:** `web`, API projections.
- **Dependencies:** M8/M10/M12 APIs.
- **Acceptance:** refresh restores authoritative state; task timeline/evidence/risk/verification visible; no dynamic script/unsafe HTML from SSE.
- **Validation:** TypeScript lint/build, component/e2e/accessibility, SSE split/reconnect/gap/duplicate tests.
- **Mapping:** B/S architecture, Chinese natural-language product experience and explainability.

## M14 — Target compatibility

- **Scope:** `targetctl probe/test/report/doctor`, safe scripts, report schema and evidence-driven Kylin adapters only if needed.
- **Files:** `cmd/targetctl`, `scripts/probe-target.sh`, `scripts/target-test.sh`, `docs/target`, target artifacts.
- **Dependencies:** Stable commands and official VM access through user-run pull workflow.
- **Acceptance:** read-only default probe/test; reports cover specified tools/platform; primary demo tested on official VM.
- **Validation:** linux/loong64 build, VM-native execution and returned report integrity.
- **Mapping:** LoongArch64 and Kylin V11.

## M15 — Benchmarks and safety evaluation

- **Scope:** `safeops-bench` intent/tool/safety/RCA/continuity/performance suites and truthful report generation.
- **Files:** `cmd/safeops-bench`, `benchmarks`, artifact schemas.
- **Dependencies:** M4-M14 stable functionality.
- **Acceptance:** all named metrics computed from real cases; absent evidence says `NOT_MEASURED`; zero unauthorized execution in suite.
- **Validation:** deterministic reruns, sample-count audit, JSON/Markdown consistency and trace coverage checks.
- **Mapping:** Quantitative competition evidence and safety quality.

## M16 — Release, deployment, README, and final audit

- **Scope:** install/uninstall/systemd, staged dual-arch release, VERSION/SHA-256/tarball, complete docs and requirement audit.
- **Files:** `deploy`, `scripts/build-release.sh`, `dist`, README/status/matrices.
- **Dependencies:** M0-M15 and target evidence.
- **Acceptance:** clean install/health/uninstall on target; exact release files; every DoD item has authoritative evidence.
- **Validation:** full test/vet/frontend/dual-build/benchmark/release, checksum verification, target install/demo/uninstall report.
- **Mapping:** Deployment, delivery, target verification and all requirements.

