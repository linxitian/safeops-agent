package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestActionPreparerPersistsExactApprovalAndSignedEnvelope(t *testing.T) {
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
	pipeline := guard.NewSafetyPipeline(catalog)
	now := time.Now().UTC()
	value := task.Task{ID: "task_prepare_action", SessionID: "ses_prepare_action", Objective: "隔离 Lab 文件", OriginalRequest: "隔离这个 Lab 文件", State: task.Investigating, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveTask(ctx, value); err != nil {
		t.Fatal(err)
	}
	target := contracts.TargetRef{Type: "file", ID: "/var/lib/safeops/lab/demo.log"}
	proposal := contracts.ActionProposal{ProposalID: "proposal_action", TaskID: value.ID, SessionID: value.SessionID, Tool: "file.quarantine", Effect: contracts.Write, Arguments: map[string]any{}, Target: target, BatchSize: 1, Reversible: true, RollbackStrategy: "restore quarantine", LabMode: true, Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: "隔离文件", PlanTargets: []contracts.TargetRef{target}}}
	snapshot := contracts.TargetSnapshot{Type: "file", ID: target.ID, CanonicalPath: target.ID, Size: 10, MTimeUnixNano: 20, Mode: 0o100600, Inode: 30}
	secret := []byte("0123456789abcdef0123456789abcdef")
	preparer := ActionPreparer{Store: store, Approvals: approvalStore, Safety: pipeline, Trace: traceWriter, Secret: secret, Now: func() time.Time { return now }}
	waiting, record, err := preparer.Prepare(ctx, value, proposal, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State != task.WaitingApproval || waiting.PendingApprovalID != record.ID || record.Status != approval.Pending {
		t.Fatalf("unexpected waiting state: %+v %+v", waiting, record)
	}
	var envelope contracts.ActionEnvelope
	if err := json.Unmarshal(waiting.PendingAction, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ApprovalID != record.ID || envelope.Nonce != record.Binding.Nonce {
		t.Fatalf("approval binding mismatch: %+v %+v", envelope, record)
	}
	if err := envelope.VerifySignature(secret); err != nil {
		t.Fatal(err)
	}
	if err := approvalStore.Validate(ctx, record.ID, record.Binding); err == nil {
		t.Fatal("pending approval was accepted as approved")
	}
	events, err := traceWriter.Read(value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].Type != trace.ApprovalRequested {
		t.Fatalf("missing approval request trace: %+v", events)
	}
}

func TestActionPreparerRejectsUnknownWrite(t *testing.T) {
	store, _ := storage.NewFileStore(t.TempDir())
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	approvalStore, _ := approval.NewStore(store.Root() + "/approvals")
	catalog, _ := guard.LoadCatalog(filepath.Join("..", "..", "policies", "tools.yaml"))
	value := task.Task{ID: "task_unknown", SessionID: "ses_unknown", State: task.Investigating}
	target := contracts.TargetRef{Type: "file", ID: "/var/lib/safeops/lab/demo"}
	proposal := contracts.ActionProposal{ProposalID: "p", TaskID: value.ID, SessionID: value.SessionID, Tool: "shell.execute", Effect: contracts.Write, Target: target, Reversible: true, RollbackStrategy: "none", Intent: contracts.IntentContext{ObjectiveTargets: []contracts.TargetRef{target}, PlanTargets: []contracts.TargetRef{target}}}
	preparer := ActionPreparer{Store: store, Approvals: approvalStore, Safety: guard.NewSafetyPipeline(catalog), Trace: traceWriter, Secret: make([]byte, 32)}
	if _, _, err := preparer.Prepare(context.Background(), value, proposal, contracts.TargetSnapshot{Type: "file", ID: target.ID}); err == nil {
		t.Fatal("unknown write capability reached approval")
	}
}
