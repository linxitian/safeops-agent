# Durable Agent Research

References: [LangGraph persistence/interrupt concepts](https://docs.langchain.com/oss/python/langgraph/persistence) and [OpenHands Software Agent SDK](https://github.com/OpenHands/software-agent-sdk). SafeOps will implement equivalent contracts in Go, not import either runtime.

## Persistence semantics

LangGraph demonstrates per-step checkpoints, stable thread/checkpoint identity, parent lineage and pending writes. Resume restarts the interrupted node, not a source-code line; side effects before interruption must therefore be idempotent or isolated. Persisted node/state/interruption layouts are compatibility contracts requiring versioned migration.

OpenHands separates base state from append-only immutable events. An Action without a matching Observation represents pending/unknown work. After a crash, an uncertain external action is not blindly replayed. It also uses recent-window stuck detection and leases/fencing to prevent duplicate recovery.

Sources: [LangGraph checkpoint base](https://github.com/langchain-ai/langgraph/blob/49ae27c2ae983cfb92091b0dea9f7bc37a716479/libs/checkpoint/langgraph/checkpoint/base/__init__.py), [interrupt semantics](https://github.com/langchain-ai/langgraph/blob/49ae27c2ae983cfb92091b0dea9f7bc37a716479/libs/langgraph/langgraph/types.py), [OpenHands state](https://github.com/OpenHands/software-agent-sdk/blob/51c102b9c0348bbdd4e6a84b1ac4199e0d77f827/openhands-sdk/openhands/sdk/conversation/state.py), [event store](https://github.com/OpenHands/software-agent-sdk/blob/51c102b9c0348bbdd4e6a84b1ac4199e0d77f827/openhands-sdk/openhands/sdk/conversation/event_store.py), and [stuck detector](https://github.com/OpenHands/software-agent-sdk/blob/51c102b9c0348bbdd4e6a84b1ac4199e0d77f827/openhands-sdk/openhands/sdk/conversation/stuck_detector.py).

## SafeOps separation

Use distinct Session, Task, RunAttempt, Checkpoint and ActionLedger entities. Checkpoints carry schema/state-machine versions, lineage, persistent budgets, evidence, pending action/approval IDs, policy/agent versions and payload digest. A task lease with monotonically increasing fencing epoch prevents two workers from resuming the same task.

Approval resolution loads checkpoint/action ledger, acquires the lease, validates binding and unused/expiry state, reruns both guards and risk, revalidates the target, claims execution durably, executes, verifies, then completes/replans/rolls back. Duplicate resolution is idempotent.

An OS write cannot be made truly exactly-once by JSON alone. If a crash occurs after the external effect but before result persistence, mark outcome unknown and reconcile service/PID/file state. Irreversible uncertainty safely fails or requests D3 input.

SSE is a projection of durable events. It needs sequence/event IDs, `Last-Event-ID` replay, deduplication and gap recovery. Persist before publish; a terminal event follows terminal Task persistence.

## Test matrix

Store conformance, every allowed/forbidden transition, crash injection at each durable boundary, approval tamper/expiry/replay/target change, SSE reconnect/gap/slow consumers, duplicate workers and fencing, concurrent event append, disk/rename failure, identical tool loops and no-progress/replan exhaustion.

