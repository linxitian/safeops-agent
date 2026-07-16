package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/llm"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestGeneralRuntimeReentersPlannerWithToolResult(t *testing.T) {
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
	s := session.Session{ID: "ses_general", Name: "general", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	tools := fakeGeneralTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "读取系统负载", ServerID: "system", Tool: "system.get_load_average", Arguments: map[string]any{}, ExpectedObservation: "负载"},
		{Kind: llm.DecisionFinal, DecisionSummary: "基于负载证据完成", FinalAnswer: "当前负载已经通过真实 MCP 证据确认。"},
	}}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	if _, err := orchestrator.Prepare(ctx, "task_general", s.ID, "查看当前系统负载"); err != nil {
		t.Fatal(err)
	}
	completed, err := orchestrator.Run(ctx, "task_general", s.ID, "查看当前系统负载", nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || completed.Runtime.Iterations != 2 || completed.Runtime.ToolCalls != 1 || len(completed.Runtime.Observations) != 1 {
		t.Fatalf("unexpected runtime state: %+v", completed)
	}
	if !planner.sawObservation {
		t.Fatal("tool result did not re-enter the planner")
	}
	if err := traceWriter.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeRejectsUnavailableTool(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewFileStore(t.TempDir())
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	now := time.Now().UTC()
	s := session.Session{ID: "ses_unavailable", Name: "test", CreatedAt: now, UpdatedAt: now}
	_ = store.SaveSession(ctx, s)
	tools := fakeGeneralTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{{Kind: llm.DecisionTool, DecisionSummary: "尝试任意命令", ServerID: "system", Tool: "shell.execute", Arguments: map[string]any{}}}}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter}
	_, _ = orchestrator.Prepare(ctx, "task_unavailable", s.ID, "执行 uname")
	result, err := orchestrator.Run(ctx, "task_unavailable", s.ID, "执行 uname", nil)
	if err == nil || result.State != task.Failed {
		t.Fatalf("unavailable tool was not rejected: %+v %v", result, err)
	}
}

func TestDiscoveredSchemaValidationFailsClosed(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["limit"],"additionalProperties":false}`)
	if err := validateToolArguments(schema, map[string]any{"limit": float64(10)}); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []map[string]any{{}, {"limit": 100.0}, {"limit": "ten"}, {"limit": 1.0, "command": "sh -c id"}} {
		if err := validateToolArguments(schema, arguments); err == nil {
			t.Fatalf("invalid arguments accepted: %#v", arguments)
		}
	}
}

func TestCaptureSelectedLargeFilesPreservesOrder(t *testing.T) {
	value := task.Task{}
	captureSelectedResources(&value, "file.find_large", map[string]any{"files": []any{map[string]any{"path": "/lab/a"}, map[string]any{"path": "/lab/b"}, map[string]any{"path": "/lab/a"}}})
	if len(value.SelectedResources) != 2 || value.SelectedResources[1] != "/lab/b" {
		t.Fatalf("unexpected resources: %+v", value.SelectedResources)
	}
	captureSelectedResources(&value, "service.get_status", map[string]any{"files": []any{map[string]any{"path": "/etc/shadow"}}})
	if len(value.SelectedResources) != 2 {
		t.Fatal("non-file tool changed selected resources")
	}
}

type fakeGeneralTools struct{}

func (fakeGeneralTools) AvailableTools() []llm.ToolCapability {
	return []llm.ToolCapability{{ServerID: "system", Name: "system.get_load_average", Description: "load", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}
}
func (fakeGeneralTools) CallTool(_ context.Context, server, name string, _ any) (*mcp.CallToolResult, error) {
	if server != "system" || name != "system.get_load_average" {
		return nil, errors.New("unavailable")
	}
	return &mcp.CallToolResult{StructuredContent: map[string]any{"load": map[string]any{"one": 0.5}}}, nil
}

type sequencePlanner struct {
	decisions      []llm.Decision
	index          int
	sawObservation bool
}

func (p *sequencePlanner) Decide(_ context.Context, request llm.DecisionRequest) (llm.Decision, error) {
	if p.index > 0 && len(request.Observations) > 0 {
		p.sawObservation = true
	}
	if p.index >= len(p.decisions) {
		return llm.Decision{}, errors.New("no more decisions")
	}
	decision := p.decisions[p.index]
	p.index++
	return decision, nil
}
