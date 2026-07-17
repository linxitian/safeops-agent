package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/id"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

type ActionExecutor interface {
	Execute(context.Context, contracts.ActionEnvelope) (executor.Result, error)
}

type ActionContinuation interface {
	ContinueAfterAction(context.Context, task.Task, contracts.ActionEnvelope, executor.Result) (task.Task, bool, error)
}

type ApprovalResumer struct {
	Store        storage.Store
	Executor     ActionExecutor
	Trace        *trace.Writer
	Continuation ActionContinuation
}

func (r ApprovalResumer) Resume(ctx context.Context, record approval.Record) (task.Task, error) {
	value, err := r.Store.GetTask(ctx, record.Binding.TaskID)
	if err != nil {
		return task.Task{}, err
	}
	if value.PendingApprovalID != record.ID {
		return value, errors.New("task pending approval binding mismatch")
	}
	if value.State != task.WaitingApproval {
		if value.State == task.Completed || value.State == task.Cancelled || value.State == task.Failed {
			return value, nil
		}
		return value, fmt.Errorf("task is %s, not WAITING_APPROVAL", value.State)
	}
	if r.Trace == nil {
		return value, errors.New("approval resumer trace is not configured")
	}
	if _, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.ApprovalResolved, map[string]any{"phase": "RESULT", "approval_id": record.ID, "status": record.Status, "reason": record.Reason, "binding": record.Binding}); err != nil {
		return value, err
	}
	switch record.Status {
	case approval.Rejected, approval.Expired:
		value.PendingAction = nil
		value.PendingApprovalID = ""
		value.FailureReason = "approval " + string(record.Status)
		value.Transition(task.Cancelled)
		if err := r.Store.SaveTask(ctx, value); err != nil {
			return value, err
		}
		_, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.TaskCancelled, map[string]any{"reason": value.FailureReason, "approval_id": record.ID})
		return value, err
	case approval.Approved:
		if _, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.TaskResumed, map[string]any{"approval_id": record.ID, "checkpoint": value.Checkpoint, "pending_tool": record.Binding.Tool}); err != nil {
			return value, err
		}
		return r.executeApproved(ctx, value, record)
	default:
		return value, fmt.Errorf("approval status %s cannot resume a task", record.Status)
	}
}

func (r ApprovalResumer) executeApproved(ctx context.Context, value task.Task, record approval.Record) (task.Task, error) {
	if r.Executor == nil {
		return value, errors.New("approval resumer executor is not configured")
	}
	var envelope contracts.ActionEnvelope
	decoder := json.NewDecoder(bytes.NewReader(value.PendingAction))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return r.failExecution(ctx, value, fmt.Errorf("decode pending ActionEnvelope: %w", err))
	}
	if envelope.TaskID != value.ID || envelope.SessionID != value.SessionID || envelope.ApprovalID != record.ID {
		return r.failExecution(ctx, value, errors.New("pending ActionEnvelope correlation mismatch"))
	}
	value.Transition(task.Executing)
	if err := r.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if _, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.ExecutionStarted, map[string]any{"phase": "STARTED", "approval_id": record.ID, "proposal_id": envelope.Proposal.ProposalID, "tool": envelope.Proposal.Tool, "target": envelope.Proposal.Target}); err != nil {
		return value, err
	}
	if envelope.Proposal.Tool == "file.restore_quarantine" {
		if _, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.RollbackStarted, map[string]any{"phase": "STARTED", "approval_id": record.ID, "tool": envelope.Proposal.Tool, "target": envelope.Proposal.Target}); err != nil {
			return value, err
		}
	}
	result, err := r.Executor.Execute(ctx, envelope)
	if err != nil {
		return r.failExecution(ctx, value, err)
	}
	executionEvent, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.ExecutionFinished, map[string]any{"phase": "FINISHED", "result": result})
	if err != nil {
		return value, err
	}
	if envelope.Proposal.Tool == "file.restore_quarantine" {
		if _, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.RollbackFinished, map[string]any{"phase": "FINISHED", "status": result.Status, "action_id": result.ActionID, "verification": result.Verification}); err != nil {
			return value, err
		}
	}
	value.Transition(task.Verifying)
	if err := r.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if result.Verification == nil || !result.Verification.Verified {
		return r.failExecution(ctx, value, errors.New("real execution result has no registered verification strategy"))
	}
	verification := map[string]any{"verified": true, "dry_run": result.Mode == executor.DryRun, "result_status": result.Status, "checks": result.Verification.Checks, "evidence": result.Verification.Evidence}
	if _, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.VerificationResult, verification); err != nil {
		return value, err
	}
	value.PendingAction = nil
	value.PendingApprovalID = ""
	if value.CurrentStep >= 0 && value.CurrentStep < len(value.Plan) {
		value.Plan[value.CurrentStep].State = "COMPLETED"
		stepID := value.Plan[value.CurrentStep].ID
		if stepID != "" && !containsString(value.CompletedSteps, stepID) {
			value.CompletedSteps = append(value.CompletedSteps, stepID)
		}
	}
	finding := "审批后 dry-run 执行边界校验通过；未修改系统"
	answer := "审批已自动恢复任务；最小权限执行器完成 dry-run 校验，未执行任何系统写操作。"
	if result.Mode == executor.LabSandbox {
		finding = fmt.Sprintf("审批后 Lab 操作 %s 已执行并验证（action_id=%s）", result.Tool, result.ActionID)
		answer = fmt.Sprintf("审批已自动恢复任务；最小权限执行器完成并验证了 Lab 操作 %s（action_id=%s）。", result.Tool, result.ActionID)
	}
	value.Findings = append(value.Findings, finding)
	value.EvidenceRefs = append(value.EvidenceRefs, fmt.Sprintf("trace://%s/%d", value.ID, executionEvent.Sequence))
	if err := r.persistActionContext(ctx, value, envelope, result); err != nil {
		return r.failExecution(ctx, value, fmt.Errorf("persist post-action session context: %w", err))
	}
	if r.Continuation != nil {
		next, handled, continuationErr := r.Continuation.ContinueAfterAction(ctx, value, envelope, result)
		if continuationErr != nil {
			return r.failExecution(ctx, next, fmt.Errorf("continue approved workflow: %w", continuationErr))
		}
		if handled {
			return next, nil
		}
	}
	if err := r.completeSession(ctx, value, envelope, result, answer); err != nil {
		return r.failExecution(ctx, value, fmt.Errorf("persist post-action session context: %w", err))
	}
	value.Transition(task.Completed)
	if err := r.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if _, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.TaskCompleted, map[string]any{"completion_criteria_met": true, "dry_run": result.Mode == executor.DryRun, "execution_mode": result.Mode}); err != nil {
		return value, err
	}
	if _, err := r.Trace.Append(ctx, value.ID, value.SessionID, trace.Final, map[string]any{"answer": answer}); err != nil {
		return value, err
	}
	return value, nil
}

func (r ApprovalResumer) failExecution(ctx context.Context, value task.Task, cause error) (task.Task, error) {
	value.FailureReason = cause.Error()
	value.Transition(task.Failed)
	if err := r.Store.SaveTask(ctx, value); err != nil {
		return value, errors.Join(cause, err)
	}
	_, _ = r.Trace.Append(ctx, value.ID, value.SessionID, trace.ExecutionFinished, map[string]any{"phase": "FINISHED", "status": "FAILED", "error": cause.Error()})
	_, _ = r.Trace.Append(ctx, value.ID, value.SessionID, trace.TaskFailed, map[string]any{"error": cause.Error()})
	return value, cause
}

func (r ApprovalResumer) completeSession(ctx context.Context, value task.Task, envelope contracts.ActionEnvelope, result executor.Result, answer string) error {
	_, err := r.Store.UpdateSession(ctx, value.SessionID, func(s *session.Session) error {
		s.Messages = append(s.Messages, session.Message{ID: id.New("msg"), Role: session.RoleAssistant, Content: answer, TaskID: value.ID, CreatedAt: time.Now().UTC()})
		s.UpdatedAt = time.Now().UTC()
		return nil
	})
	return err
}

func (r ApprovalResumer) persistActionContext(ctx context.Context, value task.Task, envelope contracts.ActionEnvelope, result executor.Result) error {
	if result.Mode != executor.LabSandbox || (envelope.Proposal.Tool != "file.quarantine" && envelope.Proposal.Tool != "file.delete" && envelope.Proposal.Tool != "file.restore_quarantine") {
		return nil
	}
	evidence := result.Verification.Evidence
	original := evidence["original_path"]
	quarantined := evidence["quarantined_path"]
	quarantineID := evidence["quarantine_id"]
	_, err := r.Store.UpdateSession(ctx, value.SessionID, func(s *session.Session) error {
		if s.PinnedContext == nil {
			s.PinnedContext = map[string]string{}
		}
		switch envelope.Proposal.Tool {
		case "file.quarantine", "file.delete":
			if original == "" || quarantined == "" || quarantineID == "" || original != envelope.TargetSnapshot.CanonicalPath {
				return errors.New("verified quarantine evidence does not match the approved target")
			}
			s.PinnedContext["quarantine_id:"+original] = quarantineID
			s.PinnedContext["quarantine_path:"+original] = quarantined
		case "file.restore_quarantine":
			if original == "" || quarantined == "" || quarantineID == "" || quarantined != envelope.TargetSnapshot.CanonicalPath {
				return errors.New("verified restore evidence does not match the approved target")
			}
			delete(s.PinnedContext, "quarantine_id:"+original)
			delete(s.PinnedContext, "quarantine_path:"+original)
		}
		s.UpdatedAt = time.Now().UTC()
		return nil
	})
	return err
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
