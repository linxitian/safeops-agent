# MCP Go SDK Research

Research snapshot: 2026-07-16. SafeOps pins official [`modelcontextprotocol/go-sdk` v1.6.1](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.6.1), not `main` or a prerelease. It requires Go 1.25 and supports MCP 2025-11-25.

## Applied decisions

- Use typed `mcp.AddTool` so Go structs produce Draft 2020-12 input/output schemas, inputs are validated before handlers, structured outputs are validated, and ordinary handler errors become `IsError=true` tool results.
- A Client must inspect both the Go `error` and `CallToolResult.IsError`; they represent protocol/transport versus tool execution failure.
- `Client.Connect` performs transport connection, `initialize`, protocol negotiation, stores `InitializeResult`, and sends `notifications/initialized`. Registry must not hand-roll JSON-RPC initialization.
- Discover with `session.Tools(ctx, nil)`, the SDK's pagination iterator, rather than a single `ListTools` page.
- Server stdout is exclusively newline-delimited MCP frames; logs go to stderr.
- Client starts a developer-authored manifest command with `exec.Command(binary, args...)`, never a shell. The short initialization Context must not own the child process lifetime; `CommandTransport.Close` performs termination.
- One Client/session per plug-in isolates callbacks and lifecycle. Health uses `Ping` but readiness additionally requires Tools capability and successful discovery/classification.
- Fingerprints cover name, title, description, input/output schemas and annotations. Tool list changes trigger background rediscovery; remote annotations remain untrusted hints.

The key official sources are [`NewServer`/options and typed AddTool](https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.1/mcp/server.go), [`Client.Connect`, Tools and CallTool](https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.1/mcp/client.go), [`StdioTransport`/in-memory transport](https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.1/mcp/transport.go), [`CommandTransport`](https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.1/mcp/cmd.go), and [`Tool`/annotations](https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.1/mcp/protocol.go).

## Required protocol tests

- initialize result/ServerInfo/capabilities;
- paginated tools/list and stable schemas;
- valid structured call;
- invalid input does not enter the handler;
- handler error versus protocol error;
- unknown Tool and invalid output;
- list-changed rediscovery/fingerprint difference;
- Context cancellation and Close;
- compiled real stdio server over CommandTransport;
- stdout log pollution and shell-metacharacter arguments.

Current code covers initialize/list/call/ping in-memory and through a compiled stdio subprocess. The remaining error/list-change cases keep M2 partial.

## Compatibility risk

The SDK is pure-Go compatible in current cross-builds. This proves compilation only. Toolchain/runtime behavior and indirect dependencies still require execution on the official LoongArch Kylin VM.

