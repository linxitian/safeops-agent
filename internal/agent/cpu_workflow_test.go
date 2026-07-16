package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/rca"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestDetectCPURecoveryDistinguishesProcessorNounFromAction(t *testing.T) {
	for _, value := range []string{"处理器现在忙不忙", "查看 CPU 高不高", "CPU 占用很高吗"} {
		if detectCPURecovery(value) {
			t.Fatalf("read-only request routed to recovery: %q", value)
		}
	}
	for _, value := range []string{"CPU 占用太高，帮我处理。", "处理器很忙，帮我恢复", "修复 CPU 高占用"} {
		if !detectCPURecovery(value) {
			t.Fatalf("action request did not route to recovery: %q", value)
		}
	}
}

func TestCPURecoveryPersistsBaselineAndRequiresMeasuredDrop(t *testing.T) {
	store, approvalStore, traceWriter, orchestrator := cpuWorkflowFixture(t, 70, 8)
	ctx := context.Background()
	const request = "CPU 占用太高，帮我处理。"
	if _, err := orchestrator.Prepare(ctx, "task_cpu_recovery", "ses_cpu_recovery", request); err != nil {
		t.Fatal(err)
	}
	waiting, err := orchestrator.Run(ctx, "task_cpu_recovery", "ses_cpu_recovery", request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State != task.WaitingApproval || waiting.CurrentStep != 4 || len(waiting.WorkflowData["cpu_baseline_percent"]) == 0 {
		t.Fatalf("CPU baseline or approval checkpoint missing: %+v", waiting)
	}
	var envelope contracts.ActionEnvelope
	if err := json.Unmarshal(waiting.PendingAction, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Proposal.Tool != "process.terminate" || envelope.Risk.Level != contracts.L2 || envelope.TargetSnapshot.Executable != demoCPUExecutable || envelope.TargetSnapshot.PID != 7331 {
		t.Fatalf("unexpected CPU action envelope: %+v", envelope)
	}
	record, err := approvalStore.Resolve(ctx, waiting.PendingApprovalID, true, "approve controlled CPU process")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	resumer := ApprovalResumer{Store: store, Trace: traceWriter, Continuation: orchestrator, Executor: fakeActionExecutor{result: executor.Result{Tool: "process.terminate", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "cpu-terminate", Changed: true, Verification: &executor.Verification{Verified: true, Checks: []string{"process absent"}}, StartedAt: now, FinishedAt: now}}}
	completed, err := resumer.Resume(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || len(completed.CompletedSteps) != 7 || completed.Plan[6].State != "COMPLETED" {
		t.Fatalf("CPU workflow did not complete: %+v", completed)
	}
	if err := traceWriter.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCPURecoveryFailsClosedWhenAggregateMetricDoesNotRecover(t *testing.T) {
	store, approvalStore, traceWriter, orchestrator := cpuWorkflowFixture(t, 70, 69.8)
	ctx := context.Background()
	const request = "处理 CPU 高占用并恢复。"
	_, _ = orchestrator.Prepare(ctx, "task_cpu_no_drop", "ses_cpu_recovery", request)
	waiting, err := orchestrator.Run(ctx, "task_cpu_no_drop", "ses_cpu_recovery", request, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := approvalStore.Resolve(ctx, waiting.PendingApprovalID, true, "approve controlled CPU process")
	now := time.Now().UTC()
	resumer := ApprovalResumer{Store: store, Trace: traceWriter, Continuation: orchestrator, Executor: fakeActionExecutor{result: executor.Result{Tool: "process.terminate", Mode: executor.LabSandbox, Status: "SUCCEEDED", Verification: &executor.Verification{Verified: true}, StartedAt: now, FinishedAt: now}}}
	failed, err := resumer.Resume(ctx, record)
	if err == nil || failed.State != task.Failed {
		t.Fatalf("non-recovered metric was accepted: %+v %v", failed, err)
	}
}

func cpuWorkflowFixture(t *testing.T, baseline, after float64) (*storage.FileStore, *approval.Store, *trace.Writer, *Orchestrator) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	approvalStore, _ := approval.NewStore(store.Root() + "/approvals")
	catalog, err := guard.LoadCatalog(filepath.Join("..", "..", "policies", "tools.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSession(ctx, session.Session{ID: "ses_cpu_recovery", Name: "cpu", PinnedContext: map[string]string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	pipeline := guard.NewSafetyPipeline(catalog)
	orchestrator := &Orchestrator{Store: store, Registry: &fakeCPUWorkflowRegistry{baseline: baseline, after: after}, Safety: pipeline, Trace: traceWriter, Actions: &ActionPreparer{Store: store, Approvals: approvalStore, Safety: pipeline, Trace: traceWriter, Secret: []byte("0123456789abcdef0123456789abcdef")}, ActionTargets: fakeCPUWorkflowTargets{}}
	return store, approvalStore, traceWriter, orchestrator
}

type fakeCPUWorkflowRegistry struct {
	baseline, after float64
	cpuCalls        int
	processCalls    int
}

func (f *fakeCPUWorkflowRegistry) CallTool(_ context.Context, server, name string, _ any) (*mcp.CallToolResult, error) {
	switch server + "/" + name {
	case "system/system.get_cpu_metrics":
		f.cpuCalls++
		value := f.baseline
		if f.cpuCalls > 1 {
			value = f.after
		}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"usage_percent": value}}, nil
	case "process/process.search":
		f.processCalls++
		processes := []platform.ProcessInfo{{PID: 7331, StartTicks: 8801, Name: "safeops-cpu-hog", Executable: demoCPUExecutable, UID: 1000}}
		if f.processCalls > 1 {
			processes = nil
		}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"processes": processes, "count": len(processes)}}, nil
	case "journal/journal.query_unit":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"events": []platform.JournalEvent{{Message: "controlled CPU pressure started"}}, "count": 1}}, nil
	case "diagnostic/diagnostic.high_cpu":
		process := platform.ProcessInfo{PID: 7331, StartTicks: 8801, Name: "safeops-cpu-hog", Executable: demoCPUExecutable, UID: 1000}
		result := rca.Result{DiagnosisLevel: rca.D2, Culprit: "pid:7331:start:8801", Confidence: .5, CandidateCauses: []rca.CandidateCause{{Cause: "受控进程累计 CPU ticks 较高", Score: .5}}}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"processes": []platform.ProcessInfo{process}, "rca": result}}, nil
	default:
		return &mcp.CallToolResult{IsError: true}, nil
	}
}

type fakeCPUWorkflowTargets struct{}

func (fakeCPUWorkflowTargets) SnapshotProcess(_ context.Context, targetID string, pid int) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "process", ID: targetID, PID: pid, StartTicks: 8801, UID: 1000, Executable: demoCPUExecutable, CommandDigest: "cpu-digest"}, nil
}

func (fakeCPUWorkflowTargets) SnapshotService(_ context.Context, targetID, unit string) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "service", ID: targetID, ServiceName: unit}, nil
}
