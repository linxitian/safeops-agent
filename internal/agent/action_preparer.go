package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/id"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

type ApprovalCreator interface {
	Create(context.Context, approval.Binding, time.Duration) (approval.Record, error)
	Resolve(context.Context, string, bool, string) (approval.Record, error)
}

type ActionPreparer struct {
	Store       storage.Store
	Approvals   ApprovalCreator
	Safety      SafetyEvaluator
	Trace       *trace.Writer
	Secret      []byte
	ApprovalTTL time.Duration
	EnvelopeTTL time.Duration
	Now         func() time.Time
}

func (p ActionPreparer) Prepare(ctx context.Context, value task.Task, proposal contracts.ActionProposal, snapshot contracts.TargetSnapshot) (task.Task, approval.Record, error) {
	if proposal.TaskID != value.ID || proposal.SessionID != value.SessionID {
		return value, approval.Record{}, errors.New("proposal task/session correlation mismatch")
	}
	if proposal.Effect != contracts.Write {
		return value, approval.Record{}, errors.New("ActionPreparer only accepts write proposals")
	}
	if contracts.CanonicalTarget(proposal.Target) != contracts.CanonicalTarget(contracts.TargetRef{Type: snapshot.Type, ID: snapshot.ID}) {
		return value, approval.Record{}, errors.New("proposal target and snapshot mismatch")
	}
	if p.Safety == nil || p.Approvals == nil || p.Trace == nil {
		return value, approval.Record{}, errors.New("action preparation safety, approval, and trace dependencies are required")
	}
	safety := p.Safety.Evaluate(proposal)
	proposalDigest, err := proposal.Digest()
	if err != nil {
		return value, approval.Record{}, err
	}
	if _, err := p.Trace.Append(ctx, value.ID, value.SessionID, trace.ActionProposed, map[string]any{"proposal_id": proposal.ProposalID, "tool": proposal.Tool, "effect": proposal.Effect, "target": proposal.Target, "proposal_digest": proposalDigest}); err != nil {
		return value, approval.Record{}, err
	}
	if _, err := p.Trace.Append(ctx, value.ID, value.SessionID, trace.StaticGuardResult, safety.Static); err != nil {
		return value, approval.Record{}, err
	}
	if safety.Static.Outcome == contracts.Deny {
		return value, approval.Record{}, fmt.Errorf("static guard denied action: %s", safety.Static.Reason)
	}
	if _, err := p.Trace.Append(ctx, value.ID, value.SessionID, trace.IntentGuardResult, safety.Intent); err != nil {
		return value, approval.Record{}, err
	}
	if safety.Intent.Outcome == contracts.Deny {
		return value, approval.Record{}, fmt.Errorf("intent guard denied action: %s", safety.Intent.Reason)
	}
	if _, err := p.Trace.Append(ctx, value.ID, value.SessionID, trace.RiskEvaluated, safety.Risk); err != nil {
		return value, approval.Record{}, err
	}
	if safety.Final.Outcome != contracts.RequireApproval {
		return value, approval.Record{}, fmt.Errorf("write action must require approval, got %s", safety.Final.Outcome)
	}
	targetDigest, err := snapshot.Digest()
	if err != nil {
		return value, approval.Record{}, err
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	envelopeTTL := p.EnvelopeTTL
	if envelopeTTL <= 0 {
		envelopeTTL = 10 * time.Minute
	}
	approvalTTL := p.ApprovalTTL
	if approvalTTL <= 0 {
		approvalTTL = envelopeTTL
	}
	if approvalTTL > envelopeTTL {
		return value, approval.Record{}, errors.New("approval TTL must not exceed ActionEnvelope TTL")
	}
	envelope := contracts.ActionEnvelope{SchemaVersion: 1, TraceID: value.ID, TaskID: value.ID, SessionID: value.SessionID, Proposal: proposal, ProposalDigest: proposalDigest, TargetSnapshot: snapshot, Risk: safety.Risk, IntentDigest: safety.Intent.IntentDigest, PolicyVersion: safety.Static.PolicyVersion, ExpiresAt: now().UTC().Add(envelopeTTL), Nonce: id.New("nonce")}
	binding := approval.Binding{TaskID: value.ID, ProposalDigest: proposalDigest, TargetSnapshotDigest: targetDigest, IntentDigest: envelope.IntentDigest, PolicyVersion: envelope.PolicyVersion, RiskLevel: envelope.Risk.Level, Tool: proposal.Tool, Nonce: envelope.Nonce}
	record, err := p.Approvals.Create(ctx, binding, approvalTTL)
	if err != nil {
		return value, approval.Record{}, err
	}
	envelope.ApprovalID = record.ID
	if err := envelope.Sign(p.Secret); err != nil {
		_, _ = p.Approvals.Resolve(context.Background(), record.ID, false, "ActionEnvelope signing failed")
		return value, record, err
	}
	pending, err := contracts.MarshalEnvelope(envelope)
	if err != nil {
		_, _ = p.Approvals.Resolve(context.Background(), record.ID, false, "ActionEnvelope serialization failed")
		return value, record, err
	}
	value.PendingAction = pending
	value.PendingApprovalID = record.ID
	value.Transition(task.WaitingApproval)
	if err := p.Store.SaveTask(ctx, value); err != nil {
		_, _ = p.Approvals.Resolve(context.Background(), record.ID, false, "Task persistence failed")
		return value, record, err
	}
	if _, err := p.Trace.Append(ctx, value.ID, value.SessionID, trace.ApprovalRequested, map[string]any{"approval_id": record.ID, "binding": binding, "expires_at": record.ExpiresAt, "risk": safety.Risk}); err != nil {
		_, _ = p.Approvals.Resolve(context.Background(), record.ID, false, "Audit trace append failed")
		value.FailureReason = err.Error()
		value.Transition(task.Failed)
		_ = p.Store.SaveTask(context.Background(), value)
		return value, record, err
	}
	return value, record, nil
}
