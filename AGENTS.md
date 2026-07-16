# SafeOps Agent repository rules

## Product invariants

- Never add an arbitrary command or shell execution tool. Names such as `shell.execute`, `terminal.run`, `command.execute`, `bash.run`, and equivalents are forbidden.
- Never invoke `sh -c` or `bash -c`. Fixed binaries and individually validated arguments may only be called from `internal/platform`, the MCP registry for developer-authored manifests, or the privileged executor's fixed handlers.
- MCP servers and `safeops-server` run without root. All future privileged writes must cross the Unix socket boundary to `safeops-privexec`.
- Tool-provided risk hints are untrusted. Local policy is authoritative; unknown write tools fail closed.
- Do not persist or display model hidden chain-of-thought. Persist structured decision summaries, hypotheses, evidence, tool selection, guard results, and completion assessments.
- Target compatibility claims require a real report from the LoongArch64 Kylin V11 VM. Cross-build success is not target verification.

## Engineering workflow

- Keep `CGO_ENABLED=0` unless a documented target-compatibility decision explicitly changes it.
- Run `go test ./...`, `go vet ./...`, frontend lint/build, linux/amd64 build, and linux/loong64 build for a core milestone.
- MCP behavior must be tested over the real protocol using official SDK transports; direct handler calls are not protocol evidence.
- Persist durable state before publishing an SSE event. Never use the SSE connection lifetime as the task lifetime.
- Update `STATUS.md`, both requirements matrices, and README feature counts after a milestone changes status.

## Status language

Use only evidence-backed status: `NOT_STARTED`, `PARTIAL`, `IMPLEMENTED`, `TESTED`, or `TARGET_VERIFIED`. A function that exists but lacks a relevant test is not `TESTED`; a cross-built binary is not `TARGET_VERIFIED`.

