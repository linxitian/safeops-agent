package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/rollback"
)

type QuarantineHandler struct {
	Manager *rollback.QuarantineManager
}

func (h QuarantineHandler) Execute(ctx context.Context, envelope contracts.ActionEnvelope) (Result, error) {
	if h.Manager == nil {
		return Result{}, errors.New("quarantine manager is not configured")
	}
	started := time.Now().UTC()
	var operation rollback.Operation
	var err error
	switch envelope.Proposal.Tool {
	case "file.quarantine":
		operation, err = h.Manager.Quarantine(ctx, envelope.TaskID, envelope.Nonce, envelope.TargetSnapshot)
	case "file.restore_quarantine":
		id, ok := envelope.Proposal.Arguments["quarantine_id"].(string)
		if !ok || id == "" {
			return Result{}, errors.New("restore requires quarantine_id")
		}
		operation, err = h.Manager.Restore(ctx, id, envelope.TargetSnapshot)
	default:
		return Result{}, errors.New("unsupported quarantine handler tool")
	}
	if err != nil {
		return Result{}, err
	}
	evidence := map[string]string{"quarantine_id": operation.Manifest.ID, "original_path": operation.Manifest.OriginalPath, "quarantined_path": operation.Manifest.QuarantinedPath, "manifest_status": string(operation.Manifest.Status)}
	return Result{Tool: envelope.Proposal.Tool, Mode: LabSandbox, Status: "SUCCEEDED", Message: fmt.Sprintf("%s completed with verified atomic rename", envelope.Proposal.Tool), ActionID: operation.Manifest.ID, Changed: true, Verification: &Verification{Verified: true, Checks: operation.Checks, Evidence: evidence}, StartedAt: started, FinishedAt: time.Now().UTC()}, nil
}
