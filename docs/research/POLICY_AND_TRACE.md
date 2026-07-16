# Policy and Trace Research

References: [OpenTelemetry Go](https://github.com/open-telemetry/opentelemetry-go) snapshot `9bc6f0a6adcdae447bbf0b0a531632356c4d587a` and [OPA](https://github.com/open-policy-agent/opa) snapshot `8e2f1807ac4764f2e0ea3ed36e98e04d6cf09e51`.

## Audit authority

Neither OTel nor OPA decision logs replace the local hash trace. OTel may sample and limits retained span events; OPA logs are buffered, rate-limited and can fail/drop. The safe write order is policy evaluation → synchronous hash-chained decision event → durable Task checkpoint → continue action → optional async telemetry mirror.

OTel Context propagates task segment correlation through Agent/MCP/Guard/Executor. Approval waits should end a span; resume creates a linked segment with persistent task/resume event IDs. HTTP uses W3C TraceContext; stdio MCP and Unix ActionEnvelope explicitly carry correlation IDs. Errors require RecordError, Error status, audit event and Task transition. Sensitive content is excluded; only digests/allowlisted summaries enter telemetry.

Sources: [OTel Context/span](https://github.com/open-telemetry/opentelemetry-go/blob/9bc6f0a6adcdae447bbf0b0a531632356c4d587a/trace/context.go), [Span API](https://github.com/open-telemetry/opentelemetry-go/blob/9bc6f0a6adcdae447bbf0b0a531632356c4d587a/trace/span.go), and [Span limits](https://github.com/open-telemetry/opentelemetry-go/blob/9bc6f0a6adcdae447bbf0b0a531632356c4d587a/sdk/trace/span_limits.go).

## Policy contract

Define an engine-independent, versioned `PolicyEvaluator`. A pure-Go/YAML implementation is default; OPA/Rego is an optional adapter only after CGO-off, loong64, size/startup and memory validation. If used, prepare queries once and evaluate concurrently. Undefined results, type/compiler/evaluation errors deny.

Policy input contains stable subject/task/action/context/risk fields and digests, not arbitrary model text. Decision output includes decision ID, outcome, risk level/score, reason codes/summary, matched rule IDs, approval flag, policy version and explicit constraints. Aggregation is `DENY > REQUIRE_APPROVAL > ALLOW`; unknown writes and targets deny.

OPA prepared-query integration is documented in its [Go integration guide](https://github.com/open-policy-agent/opa/blob/8e2f1807ac4764f2e0ea3ed36e98e04d6cf09e51/docs/docs/integration.md). Decision log fields and masking inspire SafeOps allowlisted/digested audit data: [decision logs](https://github.com/open-policy-agent/opa/blob/8e2f1807ac4764f2e0ea3ed36e98e04d6cf09e51/docs/docs/management-decision-logs.md).

Policy bundles need manifest/schema version, compatible Tool schemas and file hashes. Load/compile/smoke-test into a temporary evaluator, compute version digest, atomically activate and keep last-known-good. ActionEnvelope carries policy version; stale versions fail revalidation.

## Tests

Known read allow, unknown write deny, lab versus critical target escalation, scope/batch/reversibility, prompt injection, intent target mismatch, approval invalidated by policy/target change, PID/file reuse/change, undefined/error fail closed, secret redaction, unique linked decisions, trace modify/delete/reorder, telemetry outage independence and linux/loong64 build.

