# Reference Research Summary

Research date: 2026-07-16. Research was read-only and focused on architecture, state models, protocol usage, tests and product interaction; no project was forked or copied.

| Reference | SafeOps takeaway | Explicit non-adoption |
|---|---|---|
| MCP Go SDK | Official typed tools, stdio lifecycle, pagination, errors, schema discovery | No hand-rolled MCP or remote risk authority |
| MCP Inspector/conformance ideas | Protocol-level initialize/list/call/schema checks | Direct Go handler tests are not enough |
| HolmesGPT | Staged evidence investigation, tool feedback loop, anti-repeat, uncertainty discipline | No Python Agent runtime |
| LangGraph | Checkpoints, interrupt/resume, pending writes, compatibility contracts | No Python LangGraph dependency |
| OpenHands | Immutable events, action/observation matching, leases/fencing, stuck detection | No general code/shell execution Agent |
| Dify | Separate session/message APIs, typed lifecycle events, final state reconciliation | No workflow platform dependency |
| Open WebUI | Background task recovery, compact timeline, session UX | No dynamic JS/event execution |
| node_exporter | Collector registry, injected roots, filters, self-observation and fixtures | No daemon requirement or unsupported LoongArch binary |
| osquery | Structured Linux entities and multi-source joins | No C++/SQLite runtime |
| OpenTelemetry Go | Context correlation, links across resume, optional performance telemetry | Not an audit store |
| OPA | Versioned structured policy decisions, prepared evaluation and masking concepts | No required external OPA service; optional adapter only |

Cross-cutting architectural result:

```text
typed Linux snapshots -> bounded normalized observations
-> real protocol capabilities -> durable bounded agent reducer
-> evidence/hypotheses -> locally authoritative policy
-> durable approval/action ledger -> fixed least-privilege handlers
-> verification/rollback -> immutable hash audit
-> typed replayable UI projection
```

The research changed actual implementation in three immediate ways: the MCP SDK was upgraded/pinned to stable v1.6.1; each Registry plug-in owns its Client/session; and the child process lifetime was separated from the short initialization Context. It also established the next gates: do not add writes before local policy/intent/risk; do not treat SSE as durable state; do not claim target compatibility from a cross-build.

Detailed notes:

- [MCP](MCP.md)
- [Linux perception](LINUX_PERCEPTION.md)
- [Incident RCA](INCIDENT_RCA.md)
- [Durable Agent](DURABLE_AGENT.md)
- [Chat UX](CHAT_UX.md)
- [Policy and Trace](POLICY_AND_TRACE.md)

