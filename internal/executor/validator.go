package executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/guard"
	"time"
)

type ApprovalVerifier interface {
	Validate(context.Context, string, approval.Binding) error
	Consume(context.Context, string, approval.Binding) error
}
type ScopeAuthorizer interface {
	Authorize(contracts.ActionEnvelope) error
}
type TargetRevalidator interface {
	Revalidate(context.Context, contracts.TargetSnapshot) error
}
type Validator struct {
	Secret    []byte
	Pipeline  guard.SafetyPipeline
	Nonces    *NonceStore
	Approvals ApprovalVerifier
	Scope     ScopeAuthorizer
	Targets   TargetRevalidator
	Now       func() time.Time
	MaxTTL    time.Duration
}
type Validation struct {
	Binding          approval.Binding
	ApprovalRequired bool
}

func (v Validator) Validate(ctx context.Context, envelope contracts.ActionEnvelope) (Validation, error) {
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	current := now().UTC()
	if envelope.SchemaVersion != 1 {
		return Validation{}, errors.New("unsupported ActionEnvelope schema")
	}
	if envelope.TraceID == "" || envelope.TaskID == "" || envelope.SessionID == "" {
		return Validation{}, errors.New("envelope correlation IDs are required")
	}
	if envelope.Proposal.TaskID != envelope.TaskID || envelope.Proposal.SessionID != envelope.SessionID {
		return Validation{}, errors.New("envelope task/session binding mismatch")
	}
	if envelope.Proposal.Tool == "" {
		return Validation{}, errors.New("invalid envelope tool")
	}
	if envelope.Nonce == "" {
		return Validation{}, errors.New("envelope nonce is required")
	}
	if !envelope.ExpiresAt.After(current) {
		return Validation{}, errors.New("envelope expired")
	}
	maxTTL := v.MaxTTL
	if maxTTL <= 0 {
		maxTTL = 15 * time.Minute
	}
	if envelope.ExpiresAt.After(current.Add(maxTTL)) {
		return Validation{}, errors.New("envelope expiry exceeds maximum TTL")
	}
	if err := envelope.VerifySignature(v.Secret); err != nil {
		return Validation{}, err
	}
	proposalDigest, err := envelope.Proposal.Digest()
	if err != nil {
		return Validation{}, err
	}
	if proposalDigest != envelope.ProposalDigest {
		return Validation{}, errors.New("proposal digest mismatch")
	}
	if envelope.PolicyVersion != v.Pipeline.Static.Catalog.VersionID() {
		return Validation{}, errors.New("policy version mismatch")
	}
	if contracts.CanonicalTarget(envelope.Proposal.Target) != contracts.CanonicalTarget(contracts.TargetRef{Type: envelope.TargetSnapshot.Type, ID: envelope.TargetSnapshot.ID}) {
		return Validation{}, errors.New("target snapshot binding mismatch")
	}
	safety := v.Pipeline.Evaluate(envelope.Proposal)
	if safety.Static.Outcome == contracts.Deny || safety.Intent.Outcome == contracts.Deny || safety.Final.Outcome == contracts.Deny {
		return Validation{}, fmt.Errorf("executor safety revalidation denied: %s", safety.Final.Reason)
	}
	if safety.Intent.IntentDigest != envelope.IntentDigest {
		return Validation{}, errors.New("intent digest mismatch")
	}
	if !reflect.DeepEqual(safety.Risk, envelope.Risk) {
		return Validation{}, errors.New("risk result mismatch")
	}
	snapshotDigest, err := envelope.TargetSnapshot.Digest()
	if err != nil {
		return Validation{}, err
	}
	binding := approval.Binding{TaskID: envelope.TaskID, ProposalDigest: envelope.ProposalDigest, TargetSnapshotDigest: snapshotDigest, IntentDigest: envelope.IntentDigest, PolicyVersion: envelope.PolicyVersion, RiskLevel: envelope.Risk.Level, Tool: envelope.Proposal.Tool, Nonce: envelope.Nonce}
	approvalRequired := safety.Final.Outcome == contracts.RequireApproval
	if approvalRequired {
		if envelope.ApprovalID == "" || v.Approvals == nil {
			return Validation{}, errors.New("valid approval is required")
		}
		if err := v.Approvals.Validate(ctx, envelope.ApprovalID, binding); err != nil {
			return Validation{}, err
		}
	}
	if v.Scope == nil {
		return Validation{}, errors.New("executor target scope is not configured")
	}
	if err := v.Scope.Authorize(envelope); err != nil {
		return Validation{}, fmt.Errorf("target scope denied: %w", err)
	}
	if v.Targets == nil {
		return Validation{}, errors.New("target revalidator is not configured")
	}
	if err := v.Targets.Revalidate(ctx, envelope.TargetSnapshot); err != nil {
		return Validation{}, fmt.Errorf("TARGET_CHANGED: %w", err)
	}
	if v.Nonces == nil {
		return Validation{}, errors.New("nonce store is not configured")
	}
	if err := v.Nonces.Reserve(envelope.Nonce, envelope.ExpiresAt); err != nil {
		return Validation{}, err
	}
	return Validation{Binding: binding, ApprovalRequired: approvalRequired}, nil
}
