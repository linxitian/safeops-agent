# Architecture Gap Analysis v3 — Historical M3 Snapshot

> This audit is intentionally preserved as the baseline gap record requested at project start. Its table is not the current implementation status. Use [STATUS.md](../STATUS.md) and [competition-requirements-matrix.md](competition-requirements-matrix.md) for current evidence; later milestones closed many gaps listed below.

Audit date: 2026-07-16  
Repository baseline: the requested workspace was an empty, non-Git directory and `~/AI-References` did not exist. There was no legacy implementation to preserve or migrate.

## Evidence inspected

- Complete initial worktree (`ls`, `find`, `rg --files`): no files.
- Git state: not a repository at audit start.
- Host tools: Node 24.18.0, npm 11.16.0, systemd 255, `journalctl`, `ss`, and `ip`; Go and make absent initially.
- Official SDK and reference-source research summarized in `docs/research/`.
- Current implementation tests and live vertical-slice validation captured in `STATUS.md`.

Go 1.26.4 was installed in the user directory from the official Go archive after its published SHA-256 was verified. The module targets Go 1.25 because the pinned MCP SDK supports it. `make` remains absent on this development host, so equivalent underlying validation commands were run directly.

## M3 snapshot gap

| Area | M3 snapshot evidence | Snapshot status | Required gap closure |
|---|---|---:|---|
| Five-pillar architecture | All five have real modules/tests; only the read slice is end-to-end and executor is dry-run | PARTIAL | General Agent, write approval/resume/verify/rollback, full trace/UI and demos |
| Unified perception | Typed system/process/network/systemd/journal/file/config access plus Procfs Observation baseline | PARTIAL | Normalize all domains as bounded Observation batches with partial/error budgets |
| Real MCP | Official Go SDK v1.6.1, 8 stdio Servers/39 Tools, protocol tests and live discovery | TESTED (Ubuntu) | Periodic lifecycle/dependency behavior and official-target validation |
| Plugin registry | YAML manifest, initialize/connect, paginated discovery, schema/tool-set fingerprints, ping, enable/disable and rediscovery | PARTIAL | Capability dependency checks, version history and periodic health loop |
| Contextual risk | Deterministic L0-L3 score/factors covers target, batch, reversibility, Lab and system state; tests pass | PARTIAL | Scope/resource-specific policy, benchmark and executor revalidation |
| Static/Intent Guard | Versioned fail-closed catalog, effect/target/reversibility checks, direct/evidence-related intent alignment; tests and read slice pass | PARTIAL | Broader argument policy, approval resume and write e2e |
| Least-privilege executor | Unix socket dry-run command, HMAC Envelope, nonce, policy/intent/risk/scope/target revalidation and fixed Handler registry pass tests | PARTIAL | Real Lab-only handlers, OS hardening, verification/rollback and Agent integration |
| Approval/resume | Durable exact-bound approval state and single-use consume pass tests | PARTIAL | API/UI resolution, automatic Task resume and crash reconciliation |
| Rollback | No manager | NOT_STARTED | Quarantine/restore first, explicit outcomes and trace |
| Durable Session/Task | Atomic JSON sessions/tasks and restart recovery validated for completed vertical slice | PARTIAL | Separate Message/Checkpoint/Approval stores, leases/fencing, active task recovery, reference resolution |
| Agent runtime | Two-tool deterministic read-only plan, tool results re-enter runtime, limits declared | PARTIAL | LLM-compatible provider, general ReAct/Plan-Execute, canonical loop/no-progress, replan and completion gates |
| Reasoning trace | 22-event vertical trace includes both Guard/Risk groups, SHA-256 chain and separate head; tamper/delete tests | PARTIAL | Approval/execution/verify/rollback events, redaction audit, crash-tail recovery and UI |
| Evidence Graph | Deterministic typed nodes/edges and stable snapshots feed port diagnosis | PARTIAL | Broader hypotheses, negative evidence and cross-task projection |
| D1-D3 RCA | Deterministic confidence components and port/high-CPU/disk outputs preserve missing evidence | PARTIAL | Recorded multi-root fixtures, general Agent integration and reviewed remediation |
| RAG | Pure-Go BM25 corpus returns source/score/matched terms and contributes only 10% confidence | PARTIAL | Corpus review workflow, more cases and trace/UI projection |
| Chinese Web UI | Chinese AI console supports sessions, messages, task plan/findings and SSE; production build passes | PARTIAL | Remaining pages, archive/rename/search, approval cards, replayable SSE, RCA/security/audit views |
| Demo: CPU/memory vertical slice | Live HTTP → Task → Registry → MCP → `/proc` → result → persistence → SSE → hash trace passes | TESTED (Ubuntu) | Browser e2e test and official target validation |
| Port conflict / CPU / disk demos | Not implemented | NOT_STARTED | Controlled labs and complete guarded remediation/verification loops |
| Multi-turn file demo | Not implemented | NOT_STARTED | Context reference resolution, selected resources, quarantine approval/resume and restore |
| Target tooling | Placeholder target script fails explicitly | NOT_STARTED | `targetctl` probe/test/report/doctor and safe read-only reports |
| Benchmark | Make target fails explicitly; no metrics claimed | NOT_STARTED | Six benchmark suites and real JSON/Markdown outputs |
| LoongArch | All current Go commands cross-build statically for linux/loong64 | IMPLEMENTED (build only) | Run binaries and target suite on official Kylin VM; do not promote before report |
| Release/deploy | Placeholder release script fails explicitly | NOT_STARTED | install/uninstall, systemd hardening, staged release and SHA-256 |
| README accuracy | Replaced scaffold with evidence-backed status | IMPLEMENTED | Keep synchronized at each milestone |

## M3 snapshot risks

1. A narrow, deterministic CPU/memory parser is only a validated vertical slice, not general natural-language or contextual reasoning.
2. JSON session documents currently embed messages; this is safe for the slice but should migrate to an append-only message store before long sessions.
3. SSE history is memory-only. Refresh can recover final Task/Session, but event replay across server restart and `Last-Event-ID` are not implemented.
4. Trace append failures fail the current read Task, but approval/executor/rollback lifecycle is not connected yet and therefore lacks end-to-end audit coverage.
5. No real write Handler is callable—which is safe—but approval automatic resume, verification, rollback and complete remediation demos remain absent.
6. The eight MCP domains and 39 read tools are present; periodic health/dependency/version-history behavior remains incomplete.
7. Target compatibility remains unverified despite a successful LoongArch cross-build.

## M3 priority decision

Keep the live read-only slice as the regression anchor. Next close the general durable Agent/provider gap and connect approval → dry-run executor → verification trace before adding any Lab-only real Handler; collector normalization can proceed alongside that work. UI polish remains behind safety-loop completion.
