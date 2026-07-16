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
	"safeops-agent/internal/retrieval"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestPortRecoveryRunsEvidenceTwoApprovalsAndThreeVerifications(t *testing.T) {
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
	s := session.Session{ID: "ses_port_recovery", Name: "port recovery", PinnedContext: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	pipeline := guard.NewSafetyPipeline(catalog)
	registry := &fakePortWorkflowRegistry{}
	targets := fakePortWorkflowTargets{}
	orchestrator := &Orchestrator{
		Store: store, Registry: registry, Safety: pipeline, Trace: traceWriter, ToolTimeout: time.Second,
		Actions:       &ActionPreparer{Store: store, Approvals: approvalStore, Safety: pipeline, Trace: traceWriter, Secret: secret},
		ActionTargets: targets, Health: fakePortHealth{},
	}
	const request = "为什么 Web 服务启动失败？帮我恢复。"
	if _, err := orchestrator.Prepare(ctx, "task_port_recovery", s.ID, request); err != nil {
		t.Fatal(err)
	}
	waiting, err := orchestrator.Run(ctx, "task_port_recovery", s.ID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State != task.WaitingApproval || waiting.CurrentStep != 5 || len(waiting.CompletedSteps) != 5 || len(waiting.EvidenceRefs) != 5 {
		t.Fatalf("unexpected first approval checkpoint: %+v", waiting)
	}
	var terminateEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(waiting.PendingAction, &terminateEnvelope); err != nil {
		t.Fatal(err)
	}
	if terminateEnvelope.Proposal.Tool != "process.terminate" || terminateEnvelope.Risk.Level != contracts.L2 || terminateEnvelope.TargetSnapshot.PID != 4242 || terminateEnvelope.TargetSnapshot.StartTicks != 9911 || terminateEnvelope.TargetSnapshot.Executable != demoHolderExecutable {
		t.Fatalf("terminate envelope is not exact-bound L2: %+v", terminateEnvelope)
	}
	if err := terminateEnvelope.VerifySignature(secret); err != nil {
		t.Fatal(err)
	}
	firstApproval, err := approvalStore.Resolve(ctx, waiting.PendingApprovalID, true, "approve exact holder termination")
	if err != nil {
		t.Fatal(err)
	}
	resumer := ApprovalResumer{Store: store, Trace: traceWriter, Continuation: orchestrator, Executor: fakeActionExecutor{result: executor.Result{Tool: "process.terminate", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "terminate-action", Changed: true, Verification: &executor.Verification{Verified: true, Checks: []string{"process absent"}}, StartedAt: now, FinishedAt: now}}}
	secondWaiting, err := resumer.Resume(ctx, firstApproval)
	if err != nil {
		t.Fatal(err)
	}
	if secondWaiting.State != task.WaitingApproval || secondWaiting.CurrentStep != 6 || secondWaiting.Plan[5].State != "COMPLETED" || secondWaiting.PendingApprovalID == waiting.PendingApprovalID {
		t.Fatalf("task did not automatically continue to independent service approval: %+v", secondWaiting)
	}
	var restartEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(secondWaiting.PendingAction, &restartEnvelope); err != nil {
		t.Fatal(err)
	}
	if restartEnvelope.Proposal.Tool != "service.restart" || restartEnvelope.Risk.Level != contracts.L1 || restartEnvelope.TargetSnapshot.ServiceName != demoServiceUnit {
		t.Fatalf("restart envelope is not exact-bound L1: %+v", restartEnvelope)
	}
	secondApproval, err := approvalStore.Resolve(ctx, secondWaiting.PendingApprovalID, true, "approve exact Demo service restart")
	if err != nil {
		t.Fatal(err)
	}
	resumer.Executor = fakeActionExecutor{result: executor.Result{Tool: "service.restart", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "restart-action", Changed: true, Verification: &executor.Verification{Verified: true, Checks: []string{"service active"}}, StartedAt: now, FinishedAt: now}}
	completed, err := resumer.Resume(ctx, secondApproval)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || len(completed.Plan) != 10 || len(completed.CompletedSteps) != 10 || completed.Plan[9].State != "COMPLETED" {
		t.Fatalf("port workflow did not complete every step: %+v", completed)
	}
	if registry.serviceCalls != 2 || registry.networkCalls != 2 || registry.processCalls != 1 || registry.diagnosticCalls != 1 || registry.journalCalls != 1 {
		t.Fatalf("unexpected tool-call sequence: %+v", registry)
	}
	s, err = store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Messages) != 2 || s.Messages[1].Role != session.RoleAssistant {
		t.Fatalf("final assistant message was not durable: %+v", s)
	}
	events, err := traceWriter.Read(completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[trace.Type]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	if counts[trace.TaskResumed] != 2 || counts[trace.ApprovalRequested] != 2 || counts[trace.RCAResult] != 1 || counts[trace.KnowledgeRetrieved] != 1 || counts[trace.VerificationResult] < 3 || counts[trace.Final] != 1 {
		t.Fatalf("required audit events missing: %+v", counts)
	}
	if err := traceWriter.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

type fakePortWorkflowRegistry struct {
	serviceCalls, journalCalls, networkCalls, processCalls, diagnosticCalls int
}

func (f *fakePortWorkflowRegistry) CallTool(_ context.Context, server, name string, _ any) (*mcp.CallToolResult, error) {
	switch server + "/" + name {
	case "service/service.get_status":
		f.serviceCalls++
		state, subState, pid := "failed", "failed", 0
		if f.serviceCalls > 1 {
			state, subState, pid = "active", "running", 5252
		}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"service": platform.ServiceStatus{Name: demoServiceUnit, ActiveState: state, SubState: subState, MainPID: pid}}}, nil
	case "journal/journal.query_unit":
		f.journalCalls++
		return &mcp.CallToolResult{StructuredContent: map[string]any{"events": []platform.JournalEvent{{Message: "bind: address already in use", Priority: 3}}, "count": 1, "partial": true}}, nil
	case "network/network.check_port":
		f.networkCalls++
		owner := platform.ProcessInfo{PID: 4242, StartTicks: 9911, Name: "safeops-port-holder", Executable: demoHolderExecutable, UID: 1000}
		if f.networkCalls > 1 {
			owner = platform.ProcessInfo{PID: 5252, StartTicks: 10011, Name: "safeops-demo-web", Executable: "/opt/safeops/bin/safeops-demo-web", UID: 1000}
		}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"port": demoPort, "occupied": true, "processes": []platform.ProcessInfo{owner}}}, nil
	case "process/process.find_by_port":
		f.processCalls++
		return &mcp.CallToolResult{StructuredContent: map[string]any{"processes": []platform.ProcessInfo{{PID: 4242, StartTicks: 9911, Name: "safeops-port-holder", Executable: demoHolderExecutable, UID: 1000}}, "count": 1, "partial": true}}, nil
	case "diagnostic/diagnostic.port_conflict":
		f.diagnosticCalls++
		rcaResult := rca.Result{DiagnosisLevel: rca.D1, RootCause: "端口 18081 已被其他进程占用", Confidence: .98, CandidateCauses: []rca.CandidateCause{{Cause: "端口 18081 已被其他进程占用", Score: .98}}, EvidenceRefs: []string{"service-status", "journal:0", "network-port", "process-port-owner"}}
		knowledge := []retrieval.Result{{DocumentID: "case-port-conflict-eaddrinuse", Score: 3.2, MatchedTerms: []string{"端口", "冲突"}, Source: "knowledge/cases/port-conflict.yaml", Title: "Port conflict"}}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"diagnosis": map[string]any{"rca": rcaResult, "graph": map[string]any{}}, "knowledge": knowledge}}, nil
	default:
		return &mcp.CallToolResult{IsError: true}, nil
	}
}

type fakePortWorkflowTargets struct{}

func (fakePortWorkflowTargets) SnapshotProcess(_ context.Context, targetID string, pid int) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "process", ID: targetID, PID: pid, StartTicks: 9911, UID: 1000, Executable: demoHolderExecutable, CommandDigest: "digest"}, nil
}

func (fakePortWorkflowTargets) SnapshotService(_ context.Context, targetID, unit string) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "service", ID: targetID, ServiceName: unit, ActiveState: "failed"}, nil
}

type fakePortHealth struct{}

func (fakePortHealth) Verify(_ context.Context, endpoint string) (HealthEvidence, error) {
	return HealthEvidence{URL: endpoint, StatusCode: 200, BodySHA256: "health-digest"}, nil
}
