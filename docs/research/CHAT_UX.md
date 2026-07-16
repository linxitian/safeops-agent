# Chat and Agent UX Research

References: [Dify](https://github.com/langgenius/dify) snapshot `abb9972e1960eea63041854cb6fbe15a7abe2bd6` and [Open WebUI](https://github.com/open-webui/open-webui) snapshot `ecd48e2f718220a6400ecf49eafd4867a38feb10`. SafeOps borrows state/recovery patterns, not code.

## Truth layers

1. Session/Task Store: authoritative current entity state.
2. Hash-chained audit: security facts.
3. SSE: replayable, typed UI projection.
4. Optional OpenTelemetry: performance/correlation, not audit.

Closing an SSE stream or changing conversations does not cancel an operations task. Cancel is an explicit, idempotent, audited API.

## Session and message UX

Dify separates paginated conversation summaries from paginated messages, supports server-side rename/pin state, and rereads persisted messages after streaming completion. Open WebUI separates Chat and Message entities and restores active background tasks when a view reconnects. SafeOps should start with one authoritative message store rather than competing embedded/deduplicated formats.

Sources: [Dify conversation API](https://github.com/langgenius/dify/blob/abb9972e1960eea63041854cb6fbe15a7abe2bd6/api/controllers/web/conversation.py), [message API](https://github.com/langgenius/dify/blob/abb9972e1960eea63041854cb6fbe15a7abe2bd6/api/controllers/web/message.py), [Open WebUI Chat model](https://github.com/open-webui/open-webui/blob/ecd48e2f718220a6400ecf49eafd4867a38feb10/backend/open_webui/models/chats.py), and [Message model](https://github.com/open-webui/open-webui/blob/ecd48e2f718220a6400ecf49eafd4867a38feb10/backend/open_webui/models/chat_messages.py).

Conversation branches may branch assistant text, but executed Tools, approvals, executor effects and audit events remain a single immutable history.

## Typed lifecycle stream

Do not encode every state as token text. Planned event types include message lifecycle, task/plan/step changes, tool start/completion, evidence, guard/risk, approval, execution, verification, rollback, error/end and heartbeat. Each carries schema version, event ID, task-local monotonic sequence, task/session/message/trace IDs, time and typed payload.

The front end uses native EventSource, deduplicates event IDs and reconnects with `Last-Event-ID`. After stream end it rereads authoritative Message and Task snapshots. A sequence gap triggers resync. The current slice implements typed progress events and terminal reread, but not persisted replay.

Open WebUI's compact status history suggests showing only the current Chinese stage in chat and the full timeline in the task panel: understanding, collecting, analyzing, retrieving, guarding, waiting approval, executing, verifying, rolling back, complete.

Never execute dynamic JavaScript/HTML from stream payload. Open WebUI contains a dynamic `new Function` event feature that is explicitly incompatible with SafeOps: [source](https://github.com/open-webui/open-webui/blob/ecd48e2f718220a6400ecf49eafd4867a38feb10/src/lib/components/chat/Chat.svelte).

## UX tests

Arbitrary byte splits, multiple/partial events, heartbeat/unknown/invalid events, exact reconnect replay, refresh/restart recovery, background task badges, switch-without-cancel, idempotent approval, terminal reconciliation, archive/rename/search and the invariant that no event invokes code or bypasses approval.

