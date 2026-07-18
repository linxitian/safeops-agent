package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/llm"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestDeriveGeneralReadScopeFromRequestAndSelectedResources(t *testing.T) {
	named := deriveGeneralReadScope("帮我检查 SafeOps Lab 里有哪些大日志", nil)
	if named == nil || named.Source != "request" || len(named.AuthorizedPaths) != 1 || named.AuthorizedPaths[0] != safeOpsLabReadRoot {
		t.Fatalf("named Lab scope was not derived: %+v", named)
	}
	explicit := deriveGeneralReadScope("比较 /var/log/messages 和 /var/lib/safeops/lab/demo.log。", nil)
	if explicit == nil || len(explicit.AuthorizedPaths) != 2 || !pathWithinScope("/var/log/messages", "/var/log/messages") {
		t.Fatalf("explicit path scopes were not derived: %+v", explicit)
	}
	selected := deriveGeneralReadScope("哪些建议处理？", []string{"/var/lib/safeops/lab/demo-2.log", "/var/lib/safeops/lab/demo-1.log"})
	if selected == nil || selected.Source != "session.selected_resources" || len(selected.AuthorizedPaths) != 2 {
		t.Fatalf("selected-resource scope was not derived: %+v", selected)
	}
	if expanded := deriveGeneralReadScope("查看 CPU 和内存", selected.AuthorizedPaths); expanded != nil {
		t.Fatalf("an explicit new topic was incorrectly constrained to old selected files: %+v", expanded)
	}
	if expanded := deriveGeneralReadScope("哪些服务异常？", selected.AuthorizedPaths); expanded != nil {
		t.Fatalf("an explicit service topic was incorrectly treated as a file follow-up: %+v", expanded)
	}
	if followup := deriveGeneralReadScope("which of them is important?", selected.AuthorizedPaths); followup == nil || followup.Source != "session.selected_resources" {
		t.Fatalf("an English file follow-up was confused with the substring port: %+v", followup)
	}
	negated := requestScopePaths("只看 SafeOps Lab，不要查看 /var/log，也不要读取 /etc")
	if len(negated) != 1 || negated[0] != safeOpsLabReadRoot {
		t.Fatalf("negated paths expanded the authorized scope: %+v", negated)
	}
	englishClauses := requestScopePaths("don't read /etc, read /var/log")
	if len(englishClauses) != 1 || englishClauses[0] != "/var/log" {
		t.Fatalf("ASCII punctuation did not isolate a positive path clause: %+v", englishClauses)
	}
	for _, conjunction := range []string{"don't read /etc and read /var/log", "不要查 /etc 并检查 /var/log", "不要检查 /etc 只检查 /var/log"} {
		paths := requestScopePaths(conjunction)
		if len(paths) != 1 || paths[0] != "/var/log" {
			t.Fatalf("a positive conjunction clause remained negated for %q: %+v", conjunction, paths)
		}
	}
	chineseExclusion := deriveGeneralReadScope("不要检查 /etc，只检查 /var/log", nil)
	if chineseExclusion == nil || len(chineseExclusion.AuthorizedPaths) != 1 || chineseExclusion.AuthorizedPaths[0] != "/var/log" || len(chineseExclusion.ExcludedPaths) != 1 || chineseExclusion.ExcludedPaths[0] != "/etc" {
		t.Fatalf("a common Chinese negation expanded the local scope: %+v", chineseExclusion)
	}
	for _, question := range []string{"Can you inspect /var/log?", "请检查 /var/log？", "inspect `/var/log`"} {
		paths := requestScopePaths(question)
		if len(paths) != 1 || paths[0] != "/var/log" {
			t.Fatalf("terminal question or markdown punctuation contaminated the path for %q: %+v", question, paths)
		}
	}
	excluded := deriveGeneralReadScope("inspect /var/log except /var/log/private", nil)
	if excluded == nil || len(excluded.AuthorizedPaths) != 1 || excluded.AuthorizedPaths[0] != "/var/log" || len(excluded.ExcludedPaths) != 1 || excluded.ExcludedPaths[0] != "/var/log/private" {
		t.Fatalf("a descendant exclusion was not preserved: %+v", excluded)
	}
	parentScan := llm.Decision{Kind: llm.DecisionTool, ServerID: "file", Tool: "file.find_large", Arguments: map[string]any{"path": "/var/log"}}
	if violation := validateGeneralReadScope(excluded, parentScan); violation == nil || violation.Code != "REQUEST_READ_SCOPE_EXCLUDED" {
		t.Fatalf("a parent scan could traverse an excluded subtree: %+v", violation)
	}
	allowedFile := llm.Decision{Kind: llm.DecisionTool, ServerID: "file", Tool: "file.stat", Arguments: map[string]any{"path": "/var/log/messages"}}
	if violation := validateGeneralReadScope(excluded, allowedFile); violation != nil {
		t.Fatalf("an unrelated sibling file was rejected: %+v", violation)
	}
	hostWide := llm.Decision{Kind: llm.DecisionTool, ServerID: "diagnostic", Tool: "diagnostic.build_snapshot", Arguments: map[string]any{"path": "/var/log/messages"}}
	if violation := validateGeneralReadScope(excluded, hostWide); violation == nil || violation.Code != "REQUEST_READ_SCOPE_TOOL_MISMATCH" {
		t.Fatalf("a host-wide diagnostic tool was accepted for a file-scoped request: %+v", violation)
	}
	rootDiscovery := llm.Decision{Kind: llm.DecisionTool, ServerID: "file", Tool: "file.list_roots", Arguments: map[string]any{}}
	if violation := validateGeneralReadScope(excluded, rootDiscovery); violation == nil || violation.Code != "REQUEST_READ_SCOPE_TOOL_MISMATCH" {
		t.Fatalf("global root discovery was accepted for an explicit file scope: %+v", violation)
	}
	if pathWithinScope("/var/lib/safeops/lab", "/var/lib/safeops/lab-escape/log") {
		t.Fatal("lexical sibling escaped the authorized scope")
	}
}

func TestGeneralRuntimeReplansOutOfScopeReadBeforeMCPCall(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := session.Session{ID: "ses_scope_replan", Name: "scope replan", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	tools := &scopedReadTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "改查系统日志", ServerID: "file", Tool: "file.find_large", Arguments: map[string]any{"path": "/var/log", "minimum_bytes": float64(1), "max_depth": float64(2), "limit": float64(20)}},
		{Kind: llm.DecisionTool, DecisionSummary: "限定到 SafeOps Lab", ServerID: "file", Tool: "file.find_large", Arguments: map[string]any{"path": safeOpsLabReadRoot, "minimum_bytes": float64(1), "max_depth": float64(2), "limit": float64(20)}},
		{Kind: llm.DecisionFinal, DecisionSummary: "基于范围内证据完成", FinalAnswer: "已仅根据 SafeOps Lab 的 MCP 证据完成。"},
	}}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	const request = "帮我检查 SafeOps Lab 里有哪些大日志。"
	if _, err := orchestrator.Prepare(ctx, "task_scope_replan", s.ID, request); err != nil {
		t.Fatal(err)
	}
	completed, err := orchestrator.Run(ctx, "task_scope_replan", s.ID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || completed.Runtime.Replans != 1 || completed.Runtime.ToolCalls != 1 || len(completed.Runtime.GuardFeedback) != 1 {
		t.Fatalf("scope denial was not converted into one bounded replan: %+v", completed)
	}
	if len(tools.calls) != 1 || tools.calls[0].server != "file" || tools.calls[0].tool != "file.find_large" || tools.calls[0].path != safeOpsLabReadRoot {
		t.Fatalf("out-of-scope MCP call reached the registry: %+v", tools.calls)
	}
	if len(completed.SelectedResources) != 3 || completed.SelectedResources[2] != "/var/lib/safeops/lab/demo-1.log" {
		t.Fatalf("scoped file results were not selected in order: %+v", completed.SelectedResources)
	}
	if len(planner.requests) < 2 || planner.requests[0].LocalReadScope == nil || len(planner.requests[1].GuardFeedback) != 1 {
		t.Fatalf("planner did not receive scope and denial feedback: %+v", planner.requests)
	}
	events, err := traceWriter.Read(completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	sawScopeDenial := false
	for _, event := range events {
		if event.Type == trace.IntentGuardResult && strings.Contains(string(event.Data), "REQUEST_READ_SCOPE_MISMATCH") {
			sawScopeDenial = true
			break
		}
	}
	if !sawScopeDenial {
		t.Fatal("scope denial was not recorded in the audit trace")
	}
	if err := traceWriter.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeRepeatedScopeViolationsFailClosedWithoutEvidence(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewFileStore(t.TempDir())
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	now := time.Now().UTC()
	s := session.Session{ID: "ses_scope_fail", Name: "scope fail", CreatedAt: now, UpdatedAt: now}
	_ = store.SaveSession(ctx, s)
	outside := llm.Decision{Kind: llm.DecisionTool, DecisionSummary: "越界读取系统日志", ServerID: "file", Tool: "file.find_large", Arguments: map[string]any{"path": "/var/log", "minimum_bytes": float64(1), "max_depth": float64(2), "limit": float64(20)}}
	planner := &sequencePlanner{decisions: []llm.Decision{outside, outside, outside, outside}}
	tools := &scopedReadTools{}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	const request = "检查 SafeOps Lab 大日志"
	_, _ = orchestrator.Prepare(ctx, "task_scope_fail", s.ID, request)
	failed, err := orchestrator.Run(ctx, "task_scope_fail", s.ID, request, nil)
	if err == nil || !strings.Contains(err.Error(), "replan limit") {
		t.Fatalf("repeated scope violations did not fail closed: state=%s err=%v", failed.State, err)
	}
	if failed.State != task.Failed || failed.Runtime.ToolCalls != 0 || len(failed.Runtime.Observations) != 0 || len(failed.EvidenceRefs) != 0 || len(tools.calls) != 0 {
		t.Fatalf("scope-denied task gained MCP execution or evidence: %+v calls=%+v", failed, tools.calls)
	}
	if len(failed.Runtime.GuardFeedback) != maxRuntimeGuardFeedback {
		t.Fatalf("durable guard feedback was not bounded: %+v", failed.Runtime.GuardFeedback)
	}
	if err := traceWriter.VerifyIntegrity(failed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeConstrainsAmbiguousFollowupToSelectedFiles(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewFileStore(t.TempDir())
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	now := time.Now().UTC()
	resources := []string{"/var/lib/safeops/lab/demo-3.log", "/var/lib/safeops/lab/demo-2.log", "/var/lib/safeops/lab/demo-1.log"}
	s := session.Session{ID: "ses_selected_scope", Name: "selected scope", SelectedResources: resources, CreatedAt: now, UpdatedAt: now}
	_ = store.SaveSession(ctx, s)
	tools := &scopedReadTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "读取无关系统负载", ServerID: "system", Tool: "system.get_load_average", Arguments: map[string]any{}},
		{Kind: llm.DecisionTool, DecisionSummary: "读取第三个已选文件", ServerID: "file", Tool: "file.stat", Arguments: map[string]any{"path": resources[2]}},
		{Kind: llm.DecisionFinal, DecisionSummary: "根据已选文件证据完成", FinalAnswer: "第三个已选文件已通过 MCP 元数据核验。"},
	}}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	const request = "哪些建议处理？"
	_, _ = orchestrator.Prepare(ctx, "task_selected_scope", s.ID, request)
	completed, err := orchestrator.Run(ctx, "task_selected_scope", s.ID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || completed.Runtime.Replans != 1 || len(tools.calls) != 1 || tools.calls[0].tool != "file.stat" || tools.calls[0].path != resources[2] {
		t.Fatalf("follow-up escaped selected file resources: task=%+v calls=%+v", completed, tools.calls)
	}
	if planner.requests[0].LocalReadScope == nil || planner.requests[0].LocalReadScope.Source != "session.selected_resources" {
		t.Fatalf("selected-resource scope was not passed to planner: %+v", planner.requests[0].LocalReadScope)
	}
}

type scopedReadCall struct {
	server string
	tool   string
	path   string
}

type scopedReadTools struct {
	calls []scopedReadCall
}

func (*scopedReadTools) AvailableTools() []llm.ToolCapability {
	pathSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"minimum_bytes":{"type":"integer"},"max_depth":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"],"additionalProperties":false}`)
	return []llm.ToolCapability{
		{ServerID: "file", Name: "file.list_roots", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
		{ServerID: "file", Name: "file.find_large", InputSchema: pathSchema},
		{ServerID: "file", Name: "file.stat", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{ServerID: "system", Name: "system.get_load_average", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
	}
}

func (f *scopedReadTools) CallTool(_ context.Context, server, tool string, arguments any) (*mcp.CallToolResult, error) {
	object, ok := arguments.(map[string]any)
	if !ok {
		return nil, errors.New("arguments are not an object")
	}
	path, _ := object["path"].(string)
	f.calls = append(f.calls, scopedReadCall{server: server, tool: tool, path: path})
	switch server + "/" + tool {
	case "file/file.find_large":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"files": []any{
			map[string]any{"path": "/var/lib/safeops/lab/demo-3.log", "size_bytes": float64(3)},
			map[string]any{"path": "/var/lib/safeops/lab/demo-2.log", "size_bytes": float64(2)},
			map[string]any{"path": "/var/lib/safeops/lab/demo-1.log", "size_bytes": float64(1)},
		}, "truncated": false}}, nil
	case "file/file.stat":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"metadata": map[string]any{"path": path, "size_bytes": float64(1)}}}, nil
	case "file/file.list_roots":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"roots": []any{safeOpsLabReadRoot, "/var/log"}}}, nil
	case "system/system.get_load_average":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"load": map[string]any{"one": .5}}}, nil
	default:
		return nil, errors.New("unexpected tool")
	}
}
