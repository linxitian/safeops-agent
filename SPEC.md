# SafeOps Agent Specification

Version: 0.1-draft  
Updated: 2026-07-16

## Goals

SafeOps Agent is a Chinese B/S operations agent for LoongArch64 Kylin Advanced Server OS V11. It must complete authorized Linux operations inside a capability-oriented safety boundary through a perception-decision-guard loop:

1. collect real, structured, correlated Linux state;
2. discover and call operations capabilities over the real MCP protocol;
3. investigate with bounded ReAct and Plan-Execute loops;
4. statically assess proposals and dynamically align actions with user intent;
5. execute writes only through a least-privilege Unix-socket executor;
6. pause high-risk actions for durable human approval and resume automatically;
7. verify results and roll back only where an explicit strategy exists;
8. retain a tamper-evident, auditable decision trace without hidden chain-of-thought;
9. correlate evidence and retrieve operations knowledge for D1-D3 RCA;
10. ship and be tested on the official target VM.

## Non-goals

- An arbitrary shell, terminal, or command-generation interface.
- Guaranteeing that a language model is always correct.
- A distributed control plane or dependencies on Redis, Kafka, Neo4j, Elasticsearch, Docker, or a vector database during the competition phase.
- Saving arbitrary configuration bodies, process environments, secrets, or model hidden reasoning.
- Claiming Kylin compatibility from Ubuntu or cross-build results alone.

## Competition requirements

The five non-substitutable pillars are:

1. OS environment depth perception.
2. Real MCP operations plug-ins and registry.
3. Static and dynamic safety intent validation.
4. Least-privilege controlled execution.
5. Traceable perception-analysis-plan-guard-approval-execution-verification decisions.

Additional scored requirements include natural-language interaction, multi-source RCA, contextual risk, approval, rollback, restricted execution, LoongArch/Kylin, reproducible demos, benchmark evidence, and a Chinese B/S console. The authoritative mapping is [docs/competition-requirements-matrix.md](docs/competition-requirements-matrix.md).

## Architecture

```text
Chinese Web Console
  -> Session / Message API
  -> Durable Task Engine + Checkpoints
  -> Agent Orchestrator (bounded ReAct / Plan-Execute)
  -> MCP Plugin Registry -> stdio MCP Servers
  -> Linux Platform -> Collectors -> Observations
  -> Evidence Graph + Retriever + D1-D3 RCA
  -> Action Proposal
  -> Static Guard -> Intent Guard -> Context Risk
  -> Durable Approval
  -> ActionEnvelope
  -> Unix socket safeops-privexec -> fixed handlers
  -> Verification -> optional Rollback
  -> Hash-chained Audit Trace + replayable SSE projection
```

Dependency direction is inward: OS access is centralized in `internal/platform`; collectors normalize typed platform snapshots; Agent/RCA/API consume observations and MCP results rather than executing operating-system commands.

## Security invariants

- The model receives capability tools, never an arbitrary shell.
- Every tool has a schema, local effect classification, timeout, base risk, approval rule, rollback declaration, audit metadata, and target-test status.
- A write follows: schema validation → proposal → Static Guard → Intent Guard → contextual risk → approval when required → expiring ActionEnvelope → executor revalidation → fixed handler → verification → rollback when supported.
- Unknown writes, policy errors, undefined decisions, changed targets, replayed envelopes, expired approvals, or mismatched intent fail closed.
- Approval binds the task, action digest, arguments digest, target snapshot digest, intent digest, policy version, risk, expiry, and nonce.
- The executor exposes only `restartService`, `startService`, `stopService`, `terminateProcess`, `quarantineFile`, and `restoreQuarantineFile`; it listens only on `/run/safeops/privexec.sock`.
- L1 actions require a declared rollback strategy. Permanent quarantine purge is L3 and denied by default.
- Audit persistence failure must block a write action.

## Platform constraints

- Development: physical Ubuntu Linux, not WSL.
- Target authority: LoongArch64 Kylin V11 official VM.
- The target pulls Git changes; development must not depend on inbound SSH.
- Default build is `CGO_ENABLED=0`; dependencies must be pure Go and linux/loong64 buildable.
- OS reads prefer `/proc`, `systemctl show`, `journalctl -o json`, `ss`, and `ip`. Fixed binaries are discovered only at startup from developer-authored allowlists.
- Target-specific adapters may be added only after a real target report proves a difference.

## Durable agent requirements

- Persistent entities: Session, Message, Task, Checkpoint, Approval, Pending Action, Action Ledger, Trace, rolling summary, pinned context, and selected resources.
- Public task states: `NEW`, `INVESTIGATING`, `PLANNING`, `EXECUTING`, `WAITING_APPROVAL`, `VERIFYING`, `REPLANNING`, `COMPLETED`, `FAILED`, `CANCELLED`.
- Every tool result returns to the runtime. `FINAL` requires code-enforced completion criteria.
- Limits: 12 iterations, 30 tool calls, 10-second default tool timeout, configurable task timeout.
- Three consecutive identical tool/canonical-argument calls produce `LOOP_DETECTED`. Multiple steps with no new evidence produce `NO_PROGRESS`, then replan; repeated no-progress ends safely.
- Approval resolution reloads the checkpoint, reruns both guards and risk, revalidates the target, consumes approval once, executes, verifies, and publishes the durable result without another user message.
- On an uncertain crash window around a write, reconcile current target state; never blindly replay the action.

## Deliverables

- Go commands and modules described in `PLAN.md`.
- React/TypeScript/Vite Chinese console.
- At least 20 non-duplicative real MCP tools; competition target 25-35.
- Port-conflict, CPU-hog, bounded disk-growth, and multi-turn quarantine/restore demos.
- `targetctl`, target reports, `safeops-bench`, coverage matrices, deployment scripts, release tarball and SHA-256.
- Research, architecture, safety, operations, target, and user documentation.

## Definition of done

Done requires all core rows in the competition matrix to be `TESTED`, deployment/target rows to be `TARGET_VERIFIED`, all Go commands to build for linux/amd64 and linux/loong64, target probe/test and primary demo reports from the official VM, real benchmark reports without fabricated values, complete install/uninstall/release artifacts, and requirement-by-requirement evidence. Current status is not done; see `STATUS.md`.

