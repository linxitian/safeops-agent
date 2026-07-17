package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/rollback"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

func TestExecutorValidatesApprovalAndRejectsReplay(t *testing.T) {
	now := time.Now().UTC()
	pipeline := testPipeline(t)
	nonces, err := NewNonceStore(filepath.Join(t.TempDir(), "nonces.json"))
	if err != nil {
		t.Fatal(err)
	}
	approvals, err := approval.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	envelope := makeEnvelope(t, pipeline, now, "nonce-1")
	binding := bindingFor(t, envelope)
	record, err := approvals.Create(context.Background(), binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := approvals.Resolve(context.Background(), record.ID, true, "approved"); err != nil {
		t.Fatal(err)
	}
	envelope.ApprovalID = record.ID
	if err := envelope.Sign(testSecret); err != nil {
		t.Fatal(err)
	}
	validator := Validator{Secret: testSecret, Pipeline: pipeline, Nonces: nonces, Approvals: approvals, Scope: allowScope{}, Targets: stableTarget{}, Now: func() time.Time { return now }}
	exec := Executor{Validator: validator, Handlers: map[string]Handler{"service.restart": DryRunHandler{}}}
	result, err := exec.Execute(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "DRY_RUN_OK" || result.Mode != DryRun {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := exec.Execute(context.Background(), envelope); err == nil {
		t.Fatal("replayed envelope executed")
	}
}

func TestExecutorLabQuarantineAndRestoreEndToEnd(t *testing.T) {
	now := time.Now().UTC()
	root := t.TempDir()
	lab := filepath.Join(root, "lab")
	quarantineRoot := filepath.Join(root, "quarantine")
	if err := os.MkdirAll(lab, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lab, "large.log")
	if err := os.WriteFile(path, []byte("safeops controlled data"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := rollback.NewQuarantineManager([]string{lab}, quarantineRoot)
	if err != nil {
		t.Fatal(err)
	}
	targets := LinuxTargets{Linux: platform.NewLinux(), Commands: platform.NewCommandPlatform(), AllowedFileRoots: []string{lab, quarantineRoot}}
	pipeline := testPipeline(t)
	approvals, _ := approval.NewStore(filepath.Join(root, "approvals"))
	nonces, _ := NewNonceStore(filepath.Join(root, "nonces.json"))
	engine := Executor{Validator: Validator{Secret: testSecret, Pipeline: pipeline, Nonces: nonces, Approvals: approvals, Scope: FixedScope{AllowedFileRoots: []string{lab, quarantineRoot}}, Targets: targets, Now: func() time.Time { return now }}, Handlers: map[string]Handler{}}
	quarantineHandler := QuarantineHandler{Manager: manager}
	engine.Handlers["file.quarantine"] = quarantineHandler
	engine.Handlers["file.restore_quarantine"] = quarantineHandler
	engine.Handlers["file.delete"] = quarantineHandler
	engine.Handlers["file.create"] = FileCreateHandler{}

	quarantineSnapshot, err := targets.SnapshotFile(context.Background(), path, path)
	if err != nil {
		t.Fatal(err)
	}
	quarantineEnvelope := fileEnvelope(t, pipeline, quarantineSnapshot, "file.quarantine", map[string]any{}, "task-quarantine", "nonce-quarantine", now)
	approveEnvelope(t, approvals, &quarantineEnvelope)
	quarantined, err := engine.Execute(context.Background(), quarantineEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if quarantined.Mode != LabSandbox || !quarantined.Changed || quarantined.Verification == nil || !quarantined.Verification.Verified {
		t.Fatalf("unexpected quarantine result: %+v", quarantined)
	}
	manifest, err := manager.Get(quarantined.ActionID)
	if err != nil {
		t.Fatal(err)
	}

	restoreSnapshot, err := targets.SnapshotFile(context.Background(), manifest.QuarantinedPath, manifest.QuarantinedPath)
	if err != nil {
		t.Fatal(err)
	}
	restoreEnvelope := fileEnvelope(t, pipeline, restoreSnapshot, "file.restore_quarantine", map[string]any{"quarantine_id": manifest.ID}, "task-restore", "nonce-restore", now)
	approveEnvelope(t, approvals, &restoreEnvelope)
	restored, err := engine.Execute(context.Background(), restoreEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Mode != LabSandbox || restored.ActionID != quarantined.ActionID {
		t.Fatalf("unexpected restore result: %+v", restored)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "safeops controlled data" {
		t.Fatalf("original file was not restored: %q %v", content, err)
	}

	createPath := filepath.Join(lab, "created.txt")
	createSnapshot, err := targets.SnapshotNewFile(context.Background(), createPath, createPath)
	if err != nil {
		t.Fatal(err)
	}
	createEnvelope := fileEnvelope(t, pipeline, createSnapshot, "file.create", map[string]any{"content": "created by safeops"}, "task-create", "nonce-create", now)
	approveEnvelope(t, approvals, &createEnvelope)
	created, err := engine.Execute(context.Background(), createEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if created.Mode != LabSandbox || !created.Changed || created.Verification == nil || !created.Verification.Verified {
		t.Fatalf("unexpected create result: %+v", created)
	}
	if content, err := os.ReadFile(createPath); err != nil || string(content) != "created by safeops" {
		t.Fatalf("created file content mismatch: %q %v", content, err)
	}

	deleteSnapshot, err := targets.SnapshotFile(context.Background(), createPath, createPath)
	if err != nil {
		t.Fatal(err)
	}
	deleteEnvelope := fileEnvelope(t, pipeline, deleteSnapshot, "file.delete", map[string]any{}, "task-delete", "nonce-delete", now)
	approveEnvelope(t, approvals, &deleteEnvelope)
	deleted, err := engine.Execute(context.Background(), deleteEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Mode != LabSandbox || deleted.ActionID == "" || deleted.Verification == nil || !deleted.Verification.Verified {
		t.Fatalf("unexpected delete result: %+v", deleted)
	}
	if _, err := os.Lstat(createPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("delete did not remove active path through quarantine: %v", err)
	}
}

func fileEnvelope(t *testing.T, pipeline guard.SafetyPipeline, snapshot contracts.TargetSnapshot, tool string, arguments map[string]any, taskID, nonce string, now time.Time) contracts.ActionEnvelope {
	t.Helper()
	target := contracts.TargetRef{Type: "file", ID: snapshot.ID}
	proposal := contracts.ActionProposal{ProposalID: "proposal-" + taskID, TaskID: taskID, SessionID: "session", Tool: tool, Effect: contracts.Write, Arguments: arguments, Target: target, BatchSize: 1, Reversible: true, RollbackStrategy: "atomic quarantine rename", LabMode: true, Intent: contracts.IntentContext{OriginalRequest: "隔离或恢复 Lab 文件", Objective: "隔离或恢复 Lab 文件", ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: "执行可逆文件操作", PlanTargets: []contracts.TargetRef{target}}}
	safety := pipeline.Evaluate(proposal)
	if safety.Final.Outcome != contracts.RequireApproval {
		t.Fatalf("write proposal did not require approval: %+v", safety)
	}
	digest, err := proposal.Digest()
	if err != nil {
		t.Fatal(err)
	}
	envelope := contracts.ActionEnvelope{SchemaVersion: 1, TraceID: "trace-" + taskID, TaskID: taskID, SessionID: "session", Proposal: proposal, ProposalDigest: digest, TargetSnapshot: snapshot, Risk: safety.Risk, IntentDigest: safety.Intent.IntentDigest, PolicyVersion: pipeline.Static.Catalog.VersionID(), ExpiresAt: now.Add(5 * time.Minute), Nonce: nonce}
	if err := envelope.Sign(testSecret); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func approveEnvelope(t *testing.T, store *approval.Store, envelope *contracts.ActionEnvelope) {
	t.Helper()
	binding := bindingFor(t, *envelope)
	record, err := store.Create(context.Background(), binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(context.Background(), record.ID, true, "approved in test"); err != nil {
		t.Fatal(err)
	}
	envelope.ApprovalID = record.ID
	if err := envelope.Sign(testSecret); err != nil {
		t.Fatal(err)
	}
}

func TestValidatorRejectsTamperMissingApprovalAndTargetChange(t *testing.T) {
	now := time.Now().UTC()
	pipeline := testPipeline(t)
	newValidator := func(targets TargetRevalidator) Validator {
		nonces, err := NewNonceStore(filepath.Join(t.TempDir(), "nonces.json"))
		if err != nil {
			t.Fatal(err)
		}
		return Validator{Secret: testSecret, Pipeline: pipeline, Nonces: nonces, Scope: allowScope{}, Targets: targets, Now: func() time.Time { return now }}
	}
	missing := makeEnvelope(t, pipeline, now, "nonce-a")
	if _, err := newValidator(stableTarget{}).Validate(context.Background(), missing); err == nil {
		t.Fatal("missing approval accepted")
	}
	tampered := makeEnvelope(t, pipeline, now, "nonce-b")
	tampered.TargetSnapshot.MainPID = 99
	if _, err := newValidator(stableTarget{}).Validate(context.Background(), tampered); err == nil {
		t.Fatal("signature tamper accepted")
	}
	changed := makeEnvelope(t, pipeline, now, "nonce-c")
	approvals, err := approval.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binding := bindingFor(t, changed)
	record, err := approvals.Create(context.Background(), binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := approvals.Resolve(context.Background(), record.ID, true, "ok"); err != nil {
		t.Fatal(err)
	}
	changed.ApprovalID = record.ID
	if err := changed.Sign(testSecret); err != nil {
		t.Fatal(err)
	}
	validator := newValidator(changedTarget{})
	validator.Approvals = approvals
	if _, err := validator.Validate(context.Background(), changed); err == nil || !strings.Contains(err.Error(), "TARGET_CHANGED") {
		t.Fatalf("changed target error = %v", err)
	}
}

func testPipeline(t *testing.T) guard.SafetyPipeline {
	t.Helper()
	catalog, err := guard.LoadCatalog(filepath.Join("..", "..", "policies", "tools.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return guard.NewSafetyPipeline(catalog)
}
func makeEnvelope(t *testing.T, pipeline guard.SafetyPipeline, now time.Time, nonce string) contracts.ActionEnvelope {
	t.Helper()
	target := contracts.TargetRef{Type: "service", ID: "safeops-demo-web.service"}
	proposal := contracts.ActionProposal{ProposalID: "proposal", TaskID: "task", SessionID: "session", Tool: "service.restart", Effect: contracts.Write, Arguments: map[string]any{"unit": "safeops-demo-web.service"}, Target: target, BatchSize: 1, Reversible: false, LabMode: true, Intent: contracts.IntentContext{OriginalRequest: "恢复 Web 服务", Objective: "恢复 Web 服务", ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: "重启 Web 服务", PlanTargets: []contracts.TargetRef{target}}}
	safety := pipeline.Evaluate(proposal)
	digest, err := proposal.Digest()
	if err != nil {
		t.Fatal(err)
	}
	envelope := contracts.ActionEnvelope{SchemaVersion: 1, TraceID: "trace", TaskID: "task", SessionID: "session", Proposal: proposal, ProposalDigest: digest, TargetSnapshot: contracts.TargetSnapshot{Type: "service", ID: target.ID, ServiceName: "safeops-demo-web.service", ActiveState: "failed", MainPID: 0}, Risk: safety.Risk, IntentDigest: safety.Intent.IntentDigest, PolicyVersion: pipeline.Static.Catalog.VersionID(), ExpiresAt: now.Add(5 * time.Minute), Nonce: nonce}
	if err := envelope.Sign(testSecret); err != nil {
		t.Fatal(err)
	}
	return envelope
}
func bindingFor(t *testing.T, envelope contracts.ActionEnvelope) approval.Binding {
	t.Helper()
	digest, err := envelope.TargetSnapshot.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return approval.Binding{TaskID: envelope.TaskID, ProposalDigest: envelope.ProposalDigest, TargetSnapshotDigest: digest, IntentDigest: envelope.IntentDigest, PolicyVersion: envelope.PolicyVersion, RiskLevel: envelope.Risk.Level, Tool: envelope.Proposal.Tool, Nonce: envelope.Nonce}
}

type allowScope struct{}

func (allowScope) Authorize(contracts.ActionEnvelope) error { return nil }

type stableTarget struct{}

func (stableTarget) Revalidate(context.Context, contracts.TargetSnapshot) error { return nil }

type changedTarget struct{}

func (changedTarget) Revalidate(context.Context, contracts.TargetSnapshot) error {
	return errors.New("changed")
}
