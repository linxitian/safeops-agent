package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/contracts"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestRecoverIncompleteResumesPreparedTaskAfterStoreReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := storage.NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSession(ctx, session.Session{ID: "ses_recover_new", Name: "recover", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	before := &Orchestrator{Store: store, Registry: fakeTools{}, Safety: fakeSafety{}, Trace: traceWriter}
	if _, err := before.Prepare(ctx, "task_recover_new", "ses_recover_new", "查看 CPU"); err != nil {
		t.Fatal(err)
	}
	reopenedStore, err := storage.NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reopenedTrace, err := trace.NewWriter(reopenedStore.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	after := &Orchestrator{Store: reopenedStore, Registry: fakeTools{}, Safety: fakeSafety{}, Trace: reopenedTrace, WorkerID: "restarted", LeaseTTL: time.Minute}
	if failures := after.RecoverIncomplete(ctx, nil); len(failures) != 0 {
		t.Fatalf("restart recovery failed: %v", failures)
	}
	completed, err := reopenedStore.GetTask(ctx, "task_recover_new")
	if err != nil || completed.State != task.Completed || completed.WorkerLease.Token != "" || completed.WorkerLease.Fence != 1 {
		t.Fatalf("recovered task: %+v %v", completed, err)
	}
	if err := reopenedTrace.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRunRepairsPartialInitialTraceWithoutDuplicates(t *testing.T) {
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
	if err := store.SaveSession(ctx, session.Session{ID: "ses_partial_trace", Name: "partial", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	value := task.Task{ID: "task_partial_trace", SessionID: "ses_partial_trace", Objective: "查看 CPU", OriginalRequest: "查看 CPU", State: task.New, CreatedAt: now, UpdatedAt: now}
	if _, err := store.PrepareTask(ctx, value, func(current *session.Session) error {
		current.Messages = append(current.Messages, session.Message{ID: "msg_partial_trace", Role: session.RoleUser, Content: value.OriginalRequest, TaskID: value.ID, CreatedAt: now})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := traceWriter.Append(ctx, value.ID, value.SessionID, trace.Received, map[string]any{"request": value.OriginalRequest}); err != nil {
		t.Fatal(err)
	}
	orchestrator := &Orchestrator{Store: store, Registry: fakeTools{}, Safety: fakeSafety{}, Trace: traceWriter}
	if _, err := orchestrator.Run(ctx, value.ID, value.SessionID, value.OriginalRequest, nil); err != nil {
		t.Fatal(err)
	}
	events, err := traceWriter.Read(value.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[trace.Type]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	if len(events) < 2 || events[0].Type != trace.Received || events[1].Type != trace.TaskCreated || counts[trace.Received] != 1 || counts[trace.TaskCreated] != 1 {
		t.Fatalf("initial trace was not repaired exactly once: %+v", events)
	}
}

func TestRecoverIncompleteFailsClosedForInFlightPrivilegedAction(t *testing.T) {
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
	if err := store.SaveSession(ctx, session.Session{ID: "ses_reconcile", Name: "reconcile", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	pending, _ := json.Marshal(map[string]any{"tool": "process.terminate"})
	value := task.Task{ID: "task_reconcile", SessionID: "ses_reconcile", OriginalRequest: "处理 CPU 高占用", Objective: "处理 CPU 高占用", State: task.Executing, PendingAction: pending, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveTask(ctx, value); err != nil {
		t.Fatal(err)
	}
	orchestrator := &Orchestrator{Store: store, Trace: traceWriter, WorkerID: "restarted", LeaseTTL: time.Minute}
	failures := orchestrator.RecoverIncomplete(ctx, nil)
	if len(failures) != 1 || !strings.Contains(failures[0].Error(), "manual reconciliation") {
		t.Fatalf("unsafe recovery was not rejected: %v", failures)
	}
	failed, err := store.GetTask(ctx, value.ID)
	if err != nil || failed.State != task.Failed || failed.WorkerLease.Token != "" || len(failed.PendingAction) == 0 {
		t.Fatalf("in-flight action was not preserved for reconciliation: %+v %v", failed, err)
	}
	if !errors.Is(store.SaveTask(ctx, value), storage.ErrLeaseConflict) {
		// The original unleased snapshot is allowed only before a fence exists;
		// after recovery its stale fence must be rejected.
		t.Fatal("stale pre-recovery task snapshot crossed the persisted fence")
	}
}

func TestPrepareAndRunPersistsCompletedVerticalSlice(t *testing.T) {
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
	value := session.Session{ID: "ses_test", Name: "test", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, value); err != nil {
		t.Fatal(err)
	}
	orchestrator := &Orchestrator{Store: store, Registry: fakeTools{}, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	prepared, err := orchestrator.Prepare(ctx, "task_test", value.ID, "查看 CPU 和内存")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.State != task.New {
		t.Fatalf("unexpected prepared task: %+v", prepared)
	}
	preparedSession, err := store.GetSession(ctx, value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(preparedSession.Messages) != 1 || preparedSession.Messages[0].Role != session.RoleUser {
		t.Fatalf("request was not persisted before run: %+v", preparedSession)
	}
	var events []RuntimeEvent
	completed, err := orchestrator.Run(ctx, prepared.ID, value.ID, "查看 CPU 和内存", func(event RuntimeEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || len(completed.CompletedSteps) != 2 {
		t.Fatalf("unexpected completed task: %+v", completed)
	}
	finalSession, err := store.GetSession(ctx, value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(finalSession.Messages) != 2 || finalSession.Messages[1].Role != session.RoleAssistant {
		t.Fatalf("assistant result not persisted: %+v", finalSession)
	}
	if len(events) < 2 || events[len(events)-1].State != task.Completed {
		t.Fatalf("missing terminal event: %+v", events)
	}
	if err := traceWriter.VerifyIntegrity(prepared.ID); err != nil {
		t.Fatal(err)
	}
	traceEvents, err := traceWriter.Read(prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(traceEvents) != 22 {
		t.Fatalf("got %d trace events, want 22", len(traceEvents))
	}
}

func TestUnsupportedIntentFailsExplicitly(t *testing.T) {
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
	value := session.Session{ID: "ses_bad", Name: "test", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, value); err != nil {
		t.Fatal(err)
	}
	orchestrator := &Orchestrator{Store: store, Registry: fakeTools{}, Safety: fakeSafety{}, Trace: traceWriter}
	if _, err := orchestrator.Prepare(ctx, "task_bad", value.ID, "重启 nginx"); err != nil {
		t.Fatal(err)
	}
	result, err := orchestrator.Run(ctx, "task_bad", value.ID, "重启 nginx", nil)
	if err == nil {
		t.Fatal("unsupported write-like request was accepted")
	}
	if result.State != task.Failed {
		t.Fatalf("got state %s, want FAILED", result.State)
	}
}

type fakeTools struct{}

type fakeSafety struct{}

func (fakeSafety) Evaluate(contracts.ActionProposal) guard.PipelineResult {
	static := contracts.GuardDecision{Outcome: contracts.Allow}
	intent := guard.IntentDecision{Outcome: contracts.Allow}
	risk := contracts.RiskResult{Level: contracts.L0, Score: 5}
	final := contracts.GuardDecision{Outcome: contracts.Allow, Risk: risk}
	return guard.PipelineResult{Static: static, Intent: intent, Risk: risk, Final: final}
}

func (fakeTools) CallTool(_ context.Context, _ string, name string, _ any) (*mcp.CallToolResult, error) {
	switch name {
	case "system.get_cpu_metrics":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"usage_percent": 12.5, "cpu": map[string]any{"total_ticks": 100.0}}}, nil
	case "system.get_memory_metrics":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"usage_percent": 50.0, "memory": map[string]any{"used_bytes": float64(2 << 30), "total_bytes": float64(4 << 30), "available_bytes": float64(2 << 30)}}}, nil
	default:
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "unknown"}}}, nil
	}
}
