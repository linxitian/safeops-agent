package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestApprovedTaskAutomaticallyResumesThroughDryRun(t *testing.T) {
	ctx := context.Background()
	store, record, traceWriter := pendingApprovalFixture(t, true)
	resumer := ApprovalResumer{Store: store, Executor: fakeActionExecutor{result: executor.Result{Tool: record.Binding.Tool, Mode: executor.DryRun, Status: "DRY_RUN_OK", Verification: &executor.Verification{Verified: true}, StartedAt: time.Now(), FinishedAt: time.Now()}}, Trace: traceWriter}
	completed, err := resumer.Resume(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || completed.PendingApprovalID != "" || len(completed.Findings) != 1 {
		t.Fatalf("unexpected completed task: %+v", completed)
	}
	events, err := traceWriter.Read(completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[trace.Type]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	minimum := map[trace.Type]int{trace.ApprovalResult: 1, trace.Execution: 2, trace.Verification: 1, trace.TaskCompleted: 1, trace.Final: 1}
	for eventType, wanted := range minimum {
		if counts[eventType] < wanted {
			t.Fatalf("trace event %s count=%d, want at least %d: %+v", eventType, counts[eventType], wanted, events)
		}
	}
}

func TestRejectedApprovalCancelsWithoutExecution(t *testing.T) {
	store, record, traceWriter := pendingApprovalFixture(t, false)
	fake := &countingExecutor{}
	resumer := ApprovalResumer{Store: store, Executor: fake, Trace: traceWriter}
	result, err := resumer.Resume(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != task.Cancelled || fake.calls != 0 {
		t.Fatalf("rejected task was executed: %+v calls=%d", result, fake.calls)
	}
}

func TestApprovalResumeExecutionFailureIsDurable(t *testing.T) {
	store, record, traceWriter := pendingApprovalFixture(t, true)
	resumer := ApprovalResumer{Store: store, Executor: fakeActionExecutor{err: errors.New("executor unavailable")}, Trace: traceWriter}
	result, err := resumer.Resume(context.Background(), record)
	if err == nil || result.State != task.Failed {
		t.Fatalf("execution failure was not durable: %+v %v", result, err)
	}
	reopened, getErr := store.GetTask(context.Background(), result.ID)
	if getErr != nil || reopened.State != task.Failed {
		t.Fatalf("failed state was not persisted: %+v %v", reopened, getErr)
	}
}

func pendingApprovalFixture(t *testing.T, approve bool) (*storage.FileStore, approval.Record, *trace.Writer) {
	t.Helper()
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
	s := session.Session{ID: "ses_resume", Name: "resume", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	approvalStore, err := approval.NewStore(store.Root() + "/approvals")
	if err != nil {
		t.Fatal(err)
	}
	binding := approval.Binding{TaskID: "task_resume", ProposalDigest: "proposal", TargetSnapshotDigest: "target", IntentDigest: "intent", PolicyVersion: "policy", RiskLevel: contracts.L2, Tool: "service.restart", Nonce: "nonce"}
	record, err := approvalStore.Create(ctx, binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	record, err = approvalStore.Resolve(ctx, record.ID, approve, "operator decision")
	if err != nil {
		t.Fatal(err)
	}
	envelope := contracts.ActionEnvelope{SchemaVersion: 1, TaskID: binding.TaskID, SessionID: s.ID, ApprovalID: record.ID, Proposal: contracts.ActionProposal{ProposalID: "proposal", Tool: binding.Tool}}
	pending, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	value := task.Task{ID: binding.TaskID, SessionID: s.ID, Objective: "test", OriginalRequest: "test", State: task.WaitingApproval, PendingApprovalID: record.ID, PendingAction: pending, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveTask(ctx, value); err != nil {
		t.Fatal(err)
	}
	return store, record, traceWriter
}

type fakeActionExecutor struct {
	result executor.Result
	err    error
}

func (f fakeActionExecutor) Execute(context.Context, contracts.ActionEnvelope) (executor.Result, error) {
	return f.result, f.err
}

type countingExecutor struct{ calls int }

func (f *countingExecutor) Execute(context.Context, contracts.ActionEnvelope) (executor.Result, error) {
	f.calls++
	return executor.Result{}, nil
}
