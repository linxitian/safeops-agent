package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestFileActionResolvesOrdinalAndCarriesQuarantineContextAcrossTurns(t *testing.T) {
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
	resources := []string{"/lab/one.log", "/lab/two.log", "/lab/three.log"}
	s := session.Session{ID: "ses_file_turns", Name: "file turns", SelectedResources: resources, PinnedContext: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	preparer := &ActionPreparer{Store: store, Approvals: approvalStore, Safety: guard.NewSafetyPipeline(catalog), Trace: traceWriter, Secret: secret}
	targets := fakeFileTargets{snapshots: map[string]contracts.TargetSnapshot{
		resources[2]:      fileSnapshot(resources[2], 33),
		"/quarantine/q_3": fileSnapshot("/quarantine/q_3", 33),
	}}
	orchestrator := &Orchestrator{Store: store, Actions: preparer, FileTargets: targets, Trace: traceWriter}
	const quarantineRequest = "把第三个文件隔离起来"
	if _, err := orchestrator.Prepare(ctx, "task_quarantine_turn", s.ID, quarantineRequest); err != nil {
		t.Fatal(err)
	}
	waiting, err := orchestrator.Run(ctx, "task_quarantine_turn", s.ID, quarantineRequest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State != task.WaitingApproval || waiting.Plan[0].Tool != "file.quarantine" {
		t.Fatalf("unexpected quarantine task: %+v", waiting)
	}
	var quarantineEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(waiting.PendingAction, &quarantineEnvelope); err != nil {
		t.Fatal(err)
	}
	if quarantineEnvelope.TargetSnapshot.CanonicalPath != resources[2] {
		t.Fatalf("ordinal resolved wrong target: %+v", quarantineEnvelope.TargetSnapshot)
	}
	if err := quarantineEnvelope.VerifySignature(secret); err != nil {
		t.Fatal(err)
	}
	record, err := approvalStore.Resolve(ctx, waiting.PendingApprovalID, true, "operator approved")
	if err != nil {
		t.Fatal(err)
	}
	quarantineResult := executor.Result{Tool: "file.quarantine", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "q_3", Changed: true, Verification: &executor.Verification{Verified: true, Evidence: map[string]string{"quarantine_id": "q_3", "original_path": resources[2], "quarantined_path": "/quarantine/q_3"}}, StartedAt: now, FinishedAt: now}
	resumer := ApprovalResumer{Store: store, Executor: fakeActionExecutor{result: quarantineResult}, Trace: traceWriter}
	completed, err := resumer.Resume(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || completed.Plan[0].State != "COMPLETED" {
		t.Fatalf("quarantine did not complete: %+v", completed)
	}
	s, err = store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.PinnedContext["quarantine_id:"+resources[2]] != "q_3" || s.PinnedContext["quarantine_path:"+resources[2]] != "/quarantine/q_3" {
		t.Fatalf("quarantine context missing: %+v", s.PinnedContext)
	}

	const restoreRequest = "把第三个恢复回来"
	if _, err := orchestrator.Prepare(ctx, "task_restore_turn", s.ID, restoreRequest); err != nil {
		t.Fatal(err)
	}
	restoreWaiting, err := orchestrator.Run(ctx, "task_restore_turn", s.ID, restoreRequest, nil)
	if err != nil {
		t.Fatal(err)
	}
	var restoreEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(restoreWaiting.PendingAction, &restoreEnvelope); err != nil {
		t.Fatal(err)
	}
	if restoreWaiting.State != task.WaitingApproval || restoreEnvelope.Proposal.Tool != "file.restore_quarantine" || restoreEnvelope.Proposal.Arguments["quarantine_id"] != "q_3" || restoreEnvelope.TargetSnapshot.CanonicalPath != "/quarantine/q_3" {
		t.Fatalf("restore was not bound to quarantine context: %+v %+v", restoreWaiting, restoreEnvelope)
	}
	restoreRecord, err := approvalStore.Resolve(ctx, restoreWaiting.PendingApprovalID, true, "operator approved restore")
	if err != nil {
		t.Fatal(err)
	}
	restoreResult := executor.Result{Tool: "file.restore_quarantine", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "q_3", Changed: true, Verification: &executor.Verification{Verified: true, Evidence: map[string]string{"quarantine_id": "q_3", "original_path": resources[2], "quarantined_path": "/quarantine/q_3"}}, StartedAt: now, FinishedAt: now}
	resumer.Executor = fakeActionExecutor{result: restoreResult}
	if _, err := resumer.Resume(ctx, restoreRecord); err != nil {
		t.Fatal(err)
	}
	s, err = store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.PinnedContext["quarantine_id:"+resources[2]] != "" || s.PinnedContext["quarantine_path:"+resources[2]] != "" {
		t.Fatalf("stale quarantine context survived restore: %+v", s.PinnedContext)
	}
}

type fakeFileTargets struct {
	snapshots map[string]contracts.TargetSnapshot
}

func (f fakeFileTargets) SnapshotFile(_ context.Context, _, path string) (contracts.TargetSnapshot, error) {
	return f.snapshots[path], nil
}

func fileSnapshot(path string, inode uint64) contracts.TargetSnapshot {
	return contracts.TargetSnapshot{Type: "file", ID: path, CanonicalPath: path, Size: 10, MTimeUnixNano: 20, Mode: 0o100600, Inode: inode}
}
