package executor

import (
	"context"
	"errors"
	"fmt"
	"safeops-agent/contracts"
	"time"
)

type ExecutionMode string

const (
	DirectRestricted ExecutionMode = "DIRECT_RESTRICTED"
	LabSandbox       ExecutionMode = "LAB_SANDBOX"
	DryRun           ExecutionMode = "DRY_RUN"
)

type Result struct {
	Tool         string        `json:"tool"`
	Mode         ExecutionMode `json:"mode"`
	Status       string        `json:"status"`
	Message      string        `json:"message"`
	ActionID     string        `json:"action_id,omitempty"`
	Changed      bool          `json:"changed"`
	Verification *Verification `json:"verification,omitempty"`
	StartedAt    time.Time     `json:"started_at"`
	FinishedAt   time.Time     `json:"finished_at"`
}
type Verification struct {
	Verified bool              `json:"verified"`
	Checks   []string          `json:"checks"`
	Evidence map[string]string `json:"evidence"`
}
type Handler interface {
	Execute(context.Context, contracts.ActionEnvelope) (Result, error)
}
type Executor struct {
	Validator Validator
	Handlers  map[string]Handler
	Now       func() time.Time
}

var fixedHandlers = map[string]bool{"service.restart": true, "service.start": true, "service.stop": true, "process.terminate": true, "file.quarantine": true, "file.restore_quarantine": true}

func (e Executor) Execute(ctx context.Context, envelope contracts.ActionEnvelope) (Result, error) {
	handler := e.Handlers[envelope.Proposal.Tool]
	if handler == nil || !fixedHandlers[envelope.Proposal.Tool] {
		return Result{}, errors.New("executor has no fixed handler for tool")
	}
	validation, err := e.Validator.Validate(ctx, envelope)
	if err != nil {
		return Result{}, err
	}
	if validation.ApprovalRequired {
		if err := e.Validator.Approvals.Consume(ctx, envelope.ApprovalID, validation.Binding); err != nil {
			return Result{}, fmt.Errorf("consume approval: %w", err)
		}
	}
	return handler.Execute(ctx, envelope)
}

type DryRunHandler struct{}

func (DryRunHandler) Execute(_ context.Context, envelope contracts.ActionEnvelope) (Result, error) {
	now := time.Now().UTC()
	return Result{Tool: envelope.Proposal.Tool, Mode: DryRun, Status: "DRY_RUN_OK", Message: "固定 Handler 已完成校验；未执行系统写操作", Changed: false, Verification: &Verification{Verified: true, Checks: []string{"envelope and target validation completed"}}, StartedAt: now, FinishedAt: now}, nil
}
