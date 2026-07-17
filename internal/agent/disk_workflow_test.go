package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/rca"
	"safeops-agent/internal/safefs"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestDiskWorkflowStopsWriterQuarantinesAndDoesNotClaimPhysicalSpace(t *testing.T) {
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
	s := session.Session{ID: "ses_disk_recovery", Name: "disk", PinnedContext: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	pipeline := guard.NewSafetyPipeline(catalog)
	targets := fakeDiskWorkflowTargets{}
	orchestrator := &Orchestrator{Store: store, Registry: &fakeDiskWorkflowRegistry{}, Safety: pipeline, Trace: traceWriter, Actions: &ActionPreparer{Store: store, Approvals: approvalStore, Safety: pipeline, Trace: traceWriter, Secret: []byte("0123456789abcdef0123456789abcdef")}, ActionTargets: targets, FileTargets: targets}
	const request = "SafeOps Lab 日志异常增长，帮我处理。"
	if _, err := orchestrator.Prepare(ctx, "task_disk_recovery", s.ID, request); err != nil {
		t.Fatal(err)
	}
	firstWaiting, err := orchestrator.Run(ctx, "task_disk_recovery", s.ID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	var terminateEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(firstWaiting.PendingAction, &terminateEnvelope); err != nil {
		t.Fatal(err)
	}
	if firstWaiting.State != task.WaitingApproval || terminateEnvelope.Proposal.Tool != "process.terminate" || terminateEnvelope.Risk.Level != contracts.L2 || terminateEnvelope.TargetSnapshot.Executable != demoLogWriterExecutable {
		t.Fatalf("unexpected writer approval: %+v %+v", firstWaiting, terminateEnvelope)
	}
	firstApproval, _ := approvalStore.Resolve(ctx, firstWaiting.PendingApprovalID, true, "approve exact writer")
	resumer := ApprovalResumer{Store: store, Trace: traceWriter, Continuation: orchestrator, Executor: fakeActionExecutor{result: executor.Result{Tool: "process.terminate", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "writer-stop", Changed: true, Verification: &executor.Verification{Verified: true}, StartedAt: now, FinishedAt: now}}}
	secondWaiting, err := resumer.Resume(ctx, firstApproval)
	if err != nil {
		t.Fatal(err)
	}
	var quarantineEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(secondWaiting.PendingAction, &quarantineEnvelope); err != nil {
		t.Fatal(err)
	}
	if secondWaiting.State != task.WaitingApproval || quarantineEnvelope.Proposal.Tool != "file.quarantine" || quarantineEnvelope.Risk.Level != contracts.L1 || !quarantineEnvelope.Proposal.Reversible || quarantineEnvelope.TargetSnapshot.CanonicalPath != demoGrowthPath || quarantineEnvelope.TargetSnapshot.Size != 5<<20 {
		t.Fatalf("unexpected independent quarantine approval: %+v %+v", secondWaiting, quarantineEnvelope)
	}
	secondApproval, _ := approvalStore.Resolve(ctx, secondWaiting.PendingApprovalID, true, "approve reversible quarantine")
	resumer.Executor = fakeActionExecutor{result: executor.Result{Tool: "file.quarantine", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "q_disk", Changed: true, Verification: &executor.Verification{Verified: true, Evidence: map[string]string{"quarantine_id": "q_disk", "original_path": demoGrowthPath, "quarantined_path": "/var/lib/safeops/quarantine/objects/q_disk"}}, StartedAt: now, FinishedAt: now}}
	completed, err := resumer.Resume(ctx, secondApproval)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || len(completed.CompletedSteps) != 8 || !strings.Contains(completed.Findings[len(completed.Findings)-1], "未声称释放物理文件系统空间") {
		t.Fatalf("disk workflow did not complete honestly: %+v", completed)
	}
	s, err = store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.PinnedContext["quarantine_id:"+demoGrowthPath] != "q_disk" || len(s.SelectedResources) != 1 || s.SelectedResources[0] != demoGrowthPath || !strings.Contains(s.Messages[len(s.Messages)-1].Content, "没有伪称物理空间") {
		t.Fatalf("rollback context or honest final answer missing: %+v", s)
	}
	if err := traceWriter.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

type fakeDiskWorkflowRegistry struct {
	processCalls, fileCalls int
}

func (f *fakeDiskWorkflowRegistry) CallTool(_ context.Context, server, name string, _ any) (*mcp.CallToolResult, error) {
	switch server + "/" + name {
	case "diagnostic/diagnostic.disk_pressure":
		components := rca.ConfidenceComponents{SignalMatch: 1}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"warning": true, "disk": platform.DiskUsage{Path: demoLabRoot, TotalBytes: 100 << 20, UsedBytes: 90 << 20, FreeBytes: 10 << 20, UsedRatio: .9}, "rca": rca.Result{DiagnosisLevel: rca.D2, Confidence: components.Score(), ConfidenceComponents: components}}}, nil
	case "file/file.find_large":
		f.fileCalls++
		files := []safefs.Metadata{{Path: demoGrowthPath, Name: "growth.log", SizeBytes: 4 << 20, IsRegular: true}}
		if f.fileCalls > 1 {
			files = nil
		}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"files": files, "truncated": false}}, nil
	case "process/process.search":
		f.processCalls++
		processes := []platform.ProcessInfo{{PID: 8442, StartTicks: 5501, Name: "safeops-log-writer", Executable: demoLogWriterExecutable, Command: demoLogWriterExecutable + " -path " + demoGrowthPath, UID: 1000}}
		if f.processCalls > 1 {
			processes = nil
		}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"processes": processes, "count": len(processes)}}, nil
	case "system/system.get_disk_usage":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"disk": platform.DiskUsage{Path: demoLabRoot, TotalBytes: 100 << 20, UsedBytes: 90 << 20, FreeBytes: 10 << 20, UsedRatio: .9}}}, nil
	default:
		return &mcp.CallToolResult{IsError: true}, nil
	}
}

type fakeDiskWorkflowTargets struct{}

func (fakeDiskWorkflowTargets) SnapshotProcess(_ context.Context, targetID string, pid int) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "process", ID: targetID, PID: pid, StartTicks: 5501, UID: 1000, Executable: demoLogWriterExecutable, CommandDigest: "writer-digest"}, nil
}

func (fakeDiskWorkflowTargets) SnapshotService(_ context.Context, targetID, unit string) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "service", ID: targetID, ServiceName: unit}, nil
}

func (fakeDiskWorkflowTargets) SnapshotFile(_ context.Context, targetID, path string) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "file", ID: targetID, CanonicalPath: path, Size: 5 << 20, MTimeUnixNano: 10, Mode: 0o100600, Inode: 99}, nil
}

func (fakeDiskWorkflowTargets) SnapshotNewFile(_ context.Context, targetID, path string) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "file", ID: targetID, CanonicalPath: path, ExpectAbsent: true, ParentPath: "/lab", ParentInode: 99}, nil
}
