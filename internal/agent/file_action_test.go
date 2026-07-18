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

func TestFileActionCreatesAndDeletesAllowlistedFilesThroughApproval(t *testing.T) {
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
	existing := "/var/lib/safeops/lab/old.log"
	createPath := "/var/lib/safeops/lab/new.txt"
	s := session.Session{ID: "ses_file_create_delete", Name: "file create delete", SelectedResources: []string{existing}, PinnedContext: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	preparer := &ActionPreparer{Store: store, Approvals: approvalStore, Safety: guard.NewSafetyPipeline(catalog), Trace: traceWriter, Secret: secret}
	targets := fakeFileTargets{snapshots: map[string]contracts.TargetSnapshot{
		existing:   fileSnapshot(existing, 44),
		createPath: newFileSnapshot(createPath, 100),
	}}
	orchestrator := &Orchestrator{Store: store, Actions: preparer, FileTargets: targets, Trace: traceWriter}

	createRequest := "请新建文件 /var/lib/safeops/lab/new.txt 内容是 hello safeops"
	if _, err := orchestrator.Prepare(ctx, "task_create_file", s.ID, createRequest); err != nil {
		t.Fatal(err)
	}
	createWaiting, err := orchestrator.Run(ctx, "task_create_file", s.ID, createRequest, nil)
	if err != nil {
		t.Fatal(err)
	}
	var createEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(createWaiting.PendingAction, &createEnvelope); err != nil {
		t.Fatal(err)
	}
	if createWaiting.State != task.WaitingApproval || createEnvelope.Proposal.Tool != "file.create" || createEnvelope.TargetSnapshot.CanonicalPath != createPath || !createEnvelope.TargetSnapshot.ExpectAbsent || createEnvelope.Proposal.Arguments["content"] != "hello safeops" {
		t.Fatalf("create was not approval-bound to the new file target: %+v %+v", createWaiting, createEnvelope)
	}

	deleteRequest := "删除第一个文件"
	if _, err := orchestrator.Prepare(ctx, "task_delete_file", s.ID, deleteRequest); err != nil {
		t.Fatal(err)
	}
	deleteWaiting, err := orchestrator.Run(ctx, "task_delete_file", s.ID, deleteRequest, nil)
	if err != nil {
		t.Fatal(err)
	}
	var deleteEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(deleteWaiting.PendingAction, &deleteEnvelope); err != nil {
		t.Fatal(err)
	}
	if deleteWaiting.State != task.WaitingApproval || deleteEnvelope.Proposal.Tool != "file.delete" || deleteEnvelope.TargetSnapshot.CanonicalPath != existing {
		t.Fatalf("delete was not approval-bound to the selected file target: %+v %+v", deleteWaiting, deleteEnvelope)
	}
	record, err := approvalStore.Resolve(ctx, deleteWaiting.PendingApprovalID, true, "operator approved delete")
	if err != nil {
		t.Fatal(err)
	}
	deleteResult := executor.Result{Tool: "file.delete", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "q_delete", Changed: true, Verification: &executor.Verification{Verified: true, Evidence: map[string]string{"quarantine_id": "q_delete", "original_path": existing, "quarantined_path": "/quarantine/q_delete"}}, StartedAt: now, FinishedAt: now}
	resumer := ApprovalResumer{Store: store, Executor: fakeActionExecutor{result: deleteResult}, Trace: traceWriter}
	if _, err := resumer.Resume(ctx, record); err != nil {
		t.Fatal(err)
	}
	s, err = store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.PinnedContext["quarantine_id:"+existing] != "q_delete" || s.PinnedContext["quarantine_path:"+existing] != "/quarantine/q_delete" {
		t.Fatalf("delete did not persist restore context: %+v", s.PinnedContext)
	}
}

func TestFileCreatePersistsSelectedResourceForFollowUpDelete(t *testing.T) {
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
	createPath := "/var/lib/safeops/lab/created.txt"
	s := session.Session{ID: "ses_create_then_delete", Name: "create then delete", PinnedContext: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	preparer := &ActionPreparer{Store: store, Approvals: approvalStore, Safety: guard.NewSafetyPipeline(catalog), Trace: traceWriter, Secret: secret}
	targets := fakeFileTargets{snapshots: map[string]contracts.TargetSnapshot{
		createPath: newFileSnapshot(createPath, 100),
	}}
	orchestrator := &Orchestrator{Store: store, Actions: preparer, FileTargets: targets, Trace: traceWriter}

	createRequest := "请创建文件 /var/lib/safeops/lab/created.txt 内容是 hello"
	if _, err := orchestrator.Prepare(ctx, "task_create_followup", s.ID, createRequest); err != nil {
		t.Fatal(err)
	}
	createWaiting, err := orchestrator.Run(ctx, "task_create_followup", s.ID, createRequest, nil)
	if err != nil {
		t.Fatal(err)
	}
	createRecord, err := approvalStore.Resolve(ctx, createWaiting.PendingApprovalID, true, "operator approved create")
	if err != nil {
		t.Fatal(err)
	}
	createResult := executor.Result{Tool: "file.create", Mode: executor.LabSandbox, Status: "SUCCEEDED", ActionID: "create_action", Changed: true, Verification: &executor.Verification{Verified: true, Evidence: map[string]string{"path": createPath}}, StartedAt: now, FinishedAt: now}
	resumer := ApprovalResumer{Store: store, Executor: fakeActionExecutor{result: createResult}, Trace: traceWriter}
	if _, err := resumer.Resume(ctx, createRecord); err != nil {
		t.Fatal(err)
	}
	s, err = store.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.SelectedResources) != 1 || s.SelectedResources[0] != createPath {
		t.Fatalf("created file was not pinned as selected resource: %+v", s.SelectedResources)
	}

	targets.snapshots[createPath] = fileSnapshot(createPath, 101)
	deleteRequest := "删除该文件"
	if _, err := orchestrator.Prepare(ctx, "task_delete_followup", s.ID, deleteRequest); err != nil {
		t.Fatal(err)
	}
	deleteWaiting, err := orchestrator.Run(ctx, "task_delete_followup", s.ID, deleteRequest, nil)
	if err != nil {
		t.Fatal(err)
	}
	var deleteEnvelope contracts.ActionEnvelope
	if err := json.Unmarshal(deleteWaiting.PendingAction, &deleteEnvelope); err != nil {
		t.Fatal(err)
	}
	if deleteWaiting.State != task.WaitingApproval || deleteEnvelope.Proposal.Tool != "file.delete" || deleteEnvelope.TargetSnapshot.CanonicalPath != createPath {
		t.Fatalf("follow-up delete did not bind to created file: %+v %+v", deleteWaiting, deleteEnvelope)
	}
}

func TestParseCreateFileRequestCombinesFilenameDirectoryAndToday(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 30, 0, 0, time.Local)
	target, content, err := parseCreateFileRequestAt("创建一个2.txt，填写今天日期，在/var/lib/safeops/lab", now)
	if err != nil {
		t.Fatal(err)
	}
	if target != "/var/lib/safeops/lab/2.txt" {
		t.Fatalf("unexpected target path: %q", target)
	}
	if content != "2026-07-18" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestParseCreateFileRequestSplitsJoinedChineseActionAndFilename(t *testing.T) {
	target, content, err := parseCreateFileRequestAt("在/var/lib/safeops/lab/test创建文件1.txt", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if target != "/var/lib/safeops/lab/test/1.txt" {
		t.Fatalf("unexpected target path: %q", target)
	}
	if content != "" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestResolveFileActionTargetCombinesFilenameAndDirectory(t *testing.T) {
	target, index, source, err := resolveFileActionTarget("删除1.txt文件，在/var/lib/safeops/lab", nil)
	if err != nil {
		t.Fatal(err)
	}
	if target != "/var/lib/safeops/lab/1.txt" {
		t.Fatalf("unexpected target path: %q", target)
	}
	if index != -1 || source != "request.path" {
		t.Fatalf("unexpected resolution metadata: index=%d source=%s", index, source)
	}
}

type fakeFileTargets struct {
	snapshots map[string]contracts.TargetSnapshot
}

func (f fakeFileTargets) SnapshotFile(_ context.Context, _, path string) (contracts.TargetSnapshot, error) {
	return f.snapshots[path], nil
}

func (f fakeFileTargets) SnapshotNewFile(_ context.Context, _, path string) (contracts.TargetSnapshot, error) {
	return f.snapshots[path], nil
}

func fileSnapshot(path string, inode uint64) contracts.TargetSnapshot {
	return contracts.TargetSnapshot{Type: "file", ID: path, CanonicalPath: path, Size: 10, MTimeUnixNano: 20, Mode: 0o100600, Inode: inode}
}

func newFileSnapshot(path string, parentInode uint64) contracts.TargetSnapshot {
	return contracts.TargetSnapshot{Type: "file", ID: path, CanonicalPath: path, ExpectAbsent: true, ParentPath: filepath.Dir(path), ParentInode: parentInode}
}
