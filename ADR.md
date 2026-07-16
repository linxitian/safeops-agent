# Architecture Decision Record

Updated: 2026-07-16

## ADR-001 — Capability-oriented tools; no arbitrary shell

**Status:** Accepted.  
**Decision:** The Agent can select only named, schema-validated capabilities. Neither MCP nor the privileged executor may expose arbitrary command strings. Fixed process invocation uses a binary plus individually validated arguments and never a shell interpreter.  
**Consequences:** More domain tools and handlers must be built, but model error cannot directly become arbitrary host execution.

## ADR-002 — Official MCP Go SDK pinned at v1.6.1

**Status:** Accepted.  
**Decision:** Use `github.com/modelcontextprotocol/go-sdk/mcp` v1.6.1 and typed `mcp.AddTool`; use stdio for local plug-ins and official transports for testing. Each Registry plug-in gets a separate Client/session.  
**Consequences:** Go 1.25+ is required. `Client.Connect` performs initialization; Registry uses the paginated `Tools` iterator and checks both transport errors and `CallToolResult.IsError`. Tool hints are never security authority.

## ADR-003 — Pure-Go, single-node, file-backed durability

**Status:** Accepted for competition phase.  
**Decision:** JSON/JSONL stores with temporary file, fsync and atomic rename are the default. Keep `CGO_ENABLED=0`; no Redis/Kafka/database service.  
**Consequences:** Simple target deployment and LoongArch cross-build. Cross-process file locks, atomic mutation, crash-tail reconciliation and Task lease/fencing are implemented and tested; independent message/event indexes remain a future scale optimization.

## ADR-004 — Typed Linux Platform boundary

**Status:** Accepted.  
**Decision:** All OS reads and fixed system command invocation live in `internal/platform`. Collectors normalize typed values into Observations; Agent/RCA/API do not execute OS commands.  
**Consequences:** Fixtures can inject `/proc` and `/etc` roots. Kylin-specific code is forbidden until a target report proves a difference.

## ADR-005 — Hash-chained JSONL plus external head

**Status:** Accepted; implementation tested locally.  
**Decision:** Each event hash is `SHA256(prev_hash || canonical_event_json)`. A separately atomically written head records count and final hash so deletion of the last JSONL event is detectable.  
**Consequences:** Concurrent append, modification/deletion/reordering, complete committed tails and incomplete crash tails are tested. OTel may mirror performance data later but never replaces audit.

## ADR-006 — Audit structured decisions, not hidden reasoning

**Status:** Accepted.  
**Decision:** Store objective, plan version/step, hypotheses, evidence used/missing, selected tool, arguments digest, expected observation, completion assessment and replan reason. Never request, persist, or display model chain-of-thought.  
**Consequences:** The UI can explain and audit decisions without exposing private model reasoning.

## ADR-007 — Local policy is risk authority

**Status:** Accepted; implementation tested locally.  
**Decision:** Static Guard, Intent Guard and Context Risk produce separate structured decisions. Aggregation order is `DENY > REQUIRE_APPROVAL > ALLOW`. Unknown writes and evaluation errors deny. OPA is an optional future adapter, not a target daemon requirement.  
**Consequences:** Remote annotations remain hints; every tool needs a local policy record and version.

## ADR-008 — SSE is a projection, not task lifetime or truth

**Status:** Accepted; implementation tested locally.  
**Decision:** Persist state/events before publication. Disconnecting or switching pages does not cancel a task. The UI reloads Session/Task after terminal events.  
**Consequences:** Runtime events carry monotonic IDs and retain a bounded 200-event replay window. A missing/truncated/restarted history produces `task.gap` plus a durable Task snapshot; the client then reloads authoritative Task and Trace instead of inventing event history.

## ADR-009 — Deterministic RCA confidence

**Status:** Accepted; implementation tested locally.  
**Decision:** Default confidence is `0.4*SignalMatch + 0.3*LogPatternMatch + 0.2*GraphConsistency + 0.1*CaseSimilarity`. Models can explain and propose hypotheses, never self-report final confidence.  
**Consequences:** Weight changes require an ADR amendment and test evidence.

## ADR-010 — Approval resume and write crash semantics

**Status:** Accepted; implementation tested locally.  
**Decision:** Approval resolves a durable event and resumes from a checkpoint after re-running guards/risk and target validation. If a crash leaves an external write state uncertain, recovery fails the Task closed, preserves the pending action for manual reconciliation and never blindly replays it.  
**Consequences:** Signed Action Envelopes, nonce/idempotency, exact target snapshots, Task lease/fencing and post-state verification are mandatory and covered by local tests before Lab writes are enabled.
