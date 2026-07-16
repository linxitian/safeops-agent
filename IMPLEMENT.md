# Implementation Guide

## Current runnable slice

The repository currently implements a real read-only vertical slice:

```text
React Chinese console
-> POST Session message
-> atomic Session/Task store
-> deterministic CPU/memory intent slice
-> two-step plan
-> YAML MCP Registry
-> official SDK CommandTransport / stdio
-> mcp-system typed tools
-> LinuxPlatform /proc reads
-> tool results re-enter runtime
-> completion gate and Chinese answer
-> task/session persistence
-> SHA-256 chained Trace
-> SSE projection
```

It does not yet implement general LLM reasoning, any write, Guard, risk, approval, executor, rollback, Evidence Graph or RCA.

## Package boundaries

- `internal/platform`: typed OS access only. Future fixed invocations must use a dedicated fixed-command runner with explicit allowed binaries/arguments.
- `internal/perception`: collector lifecycle and Observation normalization; it must not embed Agent logic.
- `internal/mcpservers`: typed MCP handlers backed by Platform/services.
- `internal/registry`: developer-authored manifests, one Client/session per plug-in, discovery, health and schema fingerprinting.
- `internal/agent`: state reducer/orchestration. It calls capabilities and persists checkpoints; it does not access OS primitives.
- `internal/storage`: durable entity stores. File names use validated opaque IDs and 0600 files.
- `internal/trace`: canonical event hash chain and integrity checks.
- `internal/api`: HTTP/SSE projection; SSE disconnection never cancels a task.
- Future `guard`, `executor`, `approval`, `rollback`, `evidence`, `retrieval`, and `rca` packages must keep their contracts independent and testable.

## MCP implementation rules

Use typed `mcp.AddTool` so the official SDK derives and validates input/output schemas. Tool outputs are structured objects. The Registry:

1. reads a validated manifest;
2. starts a configured binary without a shell;
3. calls `Client.Connect` (initialize + initialized notification);
4. iterates all pages through `session.Tools`;
5. fingerprints name/title/description/input/output/annotations;
6. records Server/tool versions and health;
7. calls tools only if discovered for that server.

The process lifecycle is deliberately not bound to the short initialization timeout Context; `CommandTransport.Close` owns termination. Logs from MCP servers go to stderr because stdout carries protocol frames.

## Platform and Observation rules

Typed snapshots should retain source timestamp and stable entity identifiers. Collectors need per-collector timeout, size/entity budgets, partial errors, completeness and redaction. Expected stable identities include process boot-ID/PID/start-ticks, socket netns/inode, interface netns/ifindex, mount namespace/mount ID and file device/inode while preserving canonical path for policy.

Never read process environments by default. Treat process disappearance and permission denial as partial collection, not fabricated completeness. Bound logs and directory traversal.

## Durable transition rules

State must be written before an event is published. Every important transition increments a checkpoint. Before enabling writes, introduce append-only Message/Checkpoint/Action Ledger stores, schema/state-machine versions and task leases with fencing epochs. Iteration and tool-call budgets must survive pause/restart.

## Safety implementation sequence

No write-capable Tool should become callable until this chain is implemented and tested:

```text
ActionProposal -> schema -> StaticGuard -> IntentGuard -> RiskEngine
-> durable Approval if required -> ActionEnvelope -> executor revalidation
-> fixed handler -> durable result -> verification -> optional rollback
```

Arguments, intent, policy, target snapshot and approval are bound by digests. Executor accepts no command string. All policy and audit errors fail closed.

## Validation

Normal milestone commands are:

```bash
make test
make lint
make build-linux-amd64
make build-loong64
```

If `make` is unavailable, run the exact underlying commands from the Makefile. Target verification additionally requires `targetctl` reports from the official Kylin VM; it cannot be replaced with cross-build output.

