# Implementation Guide

## Current runnable surface

The repository now has a tested local Ubuntu implementation for the core SafeOps boundaries:

```text
React Chinese console
-> HTTP API + typed SSE
-> durable Session / Message / Task / Approval stores
-> deterministic CPU/memory slice or bounded general Agent Runtime
-> YAML MCP Registry over official stdio transports
-> 8 read-only MCP servers / 39 typed tools
-> LinuxPlatform, SafeFS and fixed systemctl/journalctl readers
-> Evidence Graph + BM25 retrieval + D1-D3 RCA
-> Action Proposal -> Static Guard -> Intent Guard -> Risk
-> durable approval and automatic resume
-> signed ActionEnvelope
-> Unix-socket safeops-privexec fixed handlers
-> verification / rollback where supported
-> hash-chained JSONL Trace and Chinese Web projections
```

The default no-credential path is still the deterministic CPU/memory vertical slice. When an OpenAI-compatible Provider is explicitly configured, the general runtime can choose only discovered L0 read tools and validates local schemas before every call. Write proposals are not MCP tools; they must pass the local safety pipeline and are executable only through fixed privileged handlers. Official Kylin runtime, target installation, and installed live Demo evidence remain pending; see `STATUS.md`.

## Package boundaries

- `internal/platform`: typed OS reads and fixed, allowlisted system command wrappers only.
- `internal/safefs`: allowlisted file metadata, directory, hash and large-file traversal primitives.
- `internal/perception`: collector lifecycle and Observation normalization; no Agent decisions.
- `internal/mcpservers`: typed MCP handlers backed by Platform/SafeFS/RCA services.
- `internal/registry`: developer-authored manifests, one MCP Client/session per plug-in, discovery, health, enable/disable, rediscovery and fingerprinting.
- `internal/agent`: deterministic slices, fixed recovery state machines, bounded general runtime, action preparation and approval resume. It calls capabilities and executor clients; it does not execute OS primitives directly.
- `internal/llm`: OpenAI-compatible structured-output provider and config loading.
- `internal/context`: selected-resource persistence and ordinal/pronoun resolution.
- `internal/session`, `internal/task`, `internal/storage`: durable state, atomic mutation, locks, leases and fencing.
- `internal/trace`: canonical event hash chain, redaction, crash-tail reconciliation and integrity checks.
- `internal/guard`: local policy, static checks, intent alignment and contextual L0-L3 risk.
- `internal/approval`: exact-bound durable approval records.
- `internal/executor`: signed envelopes, nonce replay protection, target revalidation, Unix client/server and fixed handlers.
- `internal/rollback`: quarantine manifest, restore and crash recovery.
- `internal/evidence`, `internal/retrieval`, `internal/rca`: deterministic graph, BM25 provenance and D1-D3 diagnostics.
- `internal/api`: HTTP/SSE projection; SSE disconnection never cancels a task.
- `internal/benchmark`: six auditable benchmark suites and fixed JSON/Markdown report generation.

Keep new code inside these ownership boundaries unless the contract itself is being changed.

## Provider configuration

`internal/llm.ConfigFromEnv` reads the repository or service working-directory `.env` first, then lets real environment variables override file values. Only keys prefixed with `SAFEOPS_LLM_` are accepted from `.env`.

Required together:

```text
SAFEOPS_LLM_BASE_URL
SAFEOPS_LLM_API_KEY
SAFEOPS_LLM_MODEL
```

Optional:

```text
SAFEOPS_LLM_TIMEOUT_SECONDS
```

The timeout defaults to 120 seconds and must be an integer from 1 to 600. There is no default model. If the three required values are absent, Provider creation returns `ErrNotConfigured` and the CPU/memory deterministic slice still works.

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

All 39 currently callable MCP tools are L0 read-only. File/config tools must stay confined to absolute allowlist roots from the manifest, and config tools must not return file bodies. Write action names in `policies/tools.yaml` are local contracts for the Agent and executor, not MCP capabilities exposed to the model.

## Platform and Observation rules

Typed snapshots should retain source timestamp and stable entity identifiers. Collectors need per-collector timeout, size/entity budgets, partial errors, completeness and redaction. Expected stable identities include process boot-ID/PID/start-ticks, socket netns/inode, interface netns/ifindex, mount namespace/mount ID and file device/inode while preserving canonical path for policy.

Never read process environments by default. Treat process disappearance and permission denial as partial collection, not fabricated completeness. Bound logs and directory traversal.

## Durable transition rules

State must be written before an event is published. Every important transition increments a checkpoint or durable record. Iteration budgets, tool-call budgets, pending approvals, selected resources and target snapshots must survive pause/restart. Task leases and fencing prevent duplicate workers; uncertain external write windows fail closed and require reconciliation rather than blind replay.

## Safety implementation sequence

No write-capable MCP tool should become callable. All current writes must use this chain:

```text
ActionProposal -> schema -> StaticGuard -> IntentGuard -> RiskEngine
-> durable Approval if required -> ActionEnvelope -> executor revalidation
-> fixed handler -> durable result -> verification -> optional rollback
```

Arguments, intent, policy, target snapshot and approval are bound by digests. Executor accepts no command string. All policy and audit errors fail closed.

`safeops-privexec` defaults to `dry-run`. Real mutation handlers are available only in explicit `lab` mode and are restricted to allowlisted service/process/file scenarios. Permanent purge has no handler and must remain denied by default.

## Target and release rules

Ubuntu tests, cross-builds and staged release checks are local evidence only. They do not create `TARGET_VERIFIED` status. Target compatibility requires the official LoongArch64 Kylin V11 VM workflow in `deploy/README.md`, returned `targetctl` reports, Task/Trace evidence and Demo artifacts.

Installed services use `/etc/safeops/safeops.env` as their systemd environment file. Local development can use repository-root `.env`; do not place real API keys in committed files or returned evidence.

## Validation

Normal milestone commands are:

```bash
make test
make lint
make build-linux-amd64
make build-loong64
```

If `make` is unavailable, run the exact underlying commands from the Makefile. Target verification additionally requires `targetctl` reports from the official Kylin VM; it cannot be replaced with cross-build output.
