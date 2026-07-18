# Implementation Guide

## Current runtime

The repository implements a durable read and controlled-write runtime:

```text
React Chinese console
-> POST Session message
-> atomic Session/Task store
-> deterministic workflows or bounded compatible-provider planning
-> YAML MCP Registry
-> official SDK CommandTransport / stdio
-> 8 MCP servers / 39 typed read-only tools
-> LinuxPlatform / SafeFS / fixed command adapters
-> tool results re-enter runtime
-> local Static/Intent Guard and contextual Risk
-> exact-bound Approval for controlled writes
-> signed ActionEnvelope over a Unix socket
-> fixed privileged Handler, verification, and optional rollback
-> durable task/session/approval state
-> SHA-256 chained Trace and typed SSE projection
```

Without a configured Provider, the deterministic CPU/memory slice remains
available. Without `-executor-secret`, write-action preparation remains
disabled. The privileged executor defaults to `dry-run`; explicit `lab` mode
registers only the fixed, allowlisted file, service, and process handlers.
There is no arbitrary command or Shell execution capability.

## Package boundaries

- `internal/platform`: typed OS access plus fixed, individually validated `systemctl` and `journalctl` invocations. It exposes no command string interface.
- `internal/perception`: collector lifecycle and Observation normalization; it must not embed Agent logic.
- `internal/mcpservers`: typed MCP handlers backed by Platform/services.
- `internal/registry`: developer-authored manifests, one Client/session per plug-in, discovery, health and schema fingerprinting.
- `internal/agent`: deterministic workflows, bounded general planning, action preparation, approval resume, verification, and durable recovery. It calls capabilities and does not access OS primitives directly.
- `internal/storage`: durable entity stores. File names use validated opaque IDs and 0600 files.
- `internal/trace`: canonical event hash chain and integrity checks.
- `internal/api`: HTTP/SSE projection; SSE disconnection never cancels a task.
- `internal/guard`: locally authoritative Tool catalog, Static/Intent Guard, and contextual L0-L3 risk.
- `internal/approval`: durable, exact-bound, single-consumption approval records.
- `internal/executor`: signed-envelope validation, nonce replay protection, target revalidation, Unix client/server boundary, and fixed handlers.
- `internal/rollback`: verified quarantine/restore with persistent manifests and crash recovery.
- `internal/evidence`, `internal/retrieval`, and `internal/rca`: deterministic evidence graph, BM25 provenance, and D1-D3 diagnosis.

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

Typed snapshots retain source timestamps and stable entity identifiers. Collectors enforce per-collector timeout, size/entity budgets, partial errors, completeness, and redaction. Stable identities include process PID/start-ticks/executable, socket inode, interface identity, mount identity, and file device/inode while preserving canonical paths for policy.

Never read process environments by default. Treat process disappearance and permission denial as partial collection, not fabricated completeness. Bound logs and directory traversal.

## Durable transition rules

State is written before an event is published. Task leases use fencing epochs,
approval and nonce stores survive restart, and iteration/tool-call budgets are
persisted. A process loss during an uncertain privileged-action window fails
closed and requires reconciliation instead of replaying the write.

## Safety execution sequence

Controlled writes use this implemented chain:

```text
ActionProposal -> schema -> StaticGuard -> IntentGuard -> RiskEngine
-> durable Approval if required -> ActionEnvelope -> executor revalidation
-> fixed handler -> durable result -> verification -> optional rollback
```

Arguments, intent, policy, target snapshot and approval are bound by digests. Executor accepts no command string. All policy and audit errors fail closed.

The write contracts are not MCP Tools and are not exposed as model-selectable
command capabilities. Only the fixed executor handlers registered for the
current mode may execute.

## Validation

Normal milestone commands are:

```bash
make test
make lint
make build-linux-amd64
make build-loong64
```

If `make` is unavailable, run the exact underlying commands from the Makefile. Target verification additionally requires `targetctl` reports from the official Kylin VM; it cannot be replaced with cross-build output.
