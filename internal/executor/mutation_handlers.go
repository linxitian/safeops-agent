package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/platform"
)

type ServiceRestartHandler struct {
	Commands *platform.CommandPlatform
}

func (h ServiceRestartHandler) Execute(ctx context.Context, envelope contracts.ActionEnvelope) (Result, error) {
	if h.Commands == nil || h.Commands.Runner == nil || h.Commands.Paths.Systemctl == "" {
		return Result{}, errors.New("fixed systemctl runner is unavailable")
	}
	if envelope.Proposal.Tool != "service.restart" || envelope.TargetSnapshot.Type != "service" || envelope.TargetSnapshot.ServiceName == "" {
		return Result{}, errors.New("invalid service restart envelope")
	}
	started := time.Now().UTC()
	unit := envelope.TargetSnapshot.ServiceName
	// Unit comes only from the allowlisted and revalidated snapshot. Model
	// arguments are deliberately ignored. No shell is involved.
	if _, err := h.Commands.Runner.Run(ctx, h.Commands.Paths.Systemctl, "restart", "--", unit); err != nil {
		return Result{}, err
	}
	status, err := h.Commands.Service(ctx, unit)
	if err != nil {
		return Result{}, fmt.Errorf("verify restarted service: %w", err)
	}
	if status.ActiveState != "active" {
		return Result{}, fmt.Errorf("service verification failed: active_state=%s sub_state=%s", status.ActiveState, status.SubState)
	}
	checks := []string{"fixed systemctl restart returned success", "systemctl show reports active"}
	evidence := map[string]string{"service": status.Name, "active_state": status.ActiveState, "sub_state": status.SubState, "main_pid": fmt.Sprint(status.MainPID)}
	return Result{Tool: envelope.Proposal.Tool, Mode: LabSandbox, Status: "SUCCEEDED", Message: "allowlisted service restart completed and verified", ActionID: envelope.Proposal.ProposalID, Changed: true, Verification: &Verification{Verified: true, Checks: checks, Evidence: evidence}, StartedAt: started, FinishedAt: time.Now().UTC()}, nil
}

type ProcessReader interface {
	Process(context.Context, int) (platform.ProcessInfo, error)
}

type ProcessTerminateHandler struct {
	Linux   ProcessReader
	Signal  func(int, syscall.Signal) error
	Timeout time.Duration
}

func (h ProcessTerminateHandler) Execute(ctx context.Context, envelope contracts.ActionEnvelope) (Result, error) {
	if h.Linux == nil {
		return Result{}, errors.New("process platform is unavailable")
	}
	if envelope.Proposal.Tool != "process.terminate" || envelope.TargetSnapshot.Type != "process" || envelope.TargetSnapshot.PID <= 1 {
		return Result{}, errors.New("invalid process terminate envelope")
	}
	if envelope.TargetSnapshot.PID == os.Getpid() {
		return Result{}, errors.New("executor refuses to signal itself")
	}
	signal := h.Signal
	if signal == nil {
		signal = syscall.Kill
	}
	started := time.Now().UTC()
	if err := signal(envelope.TargetSnapshot.PID, syscall.SIGTERM); err != nil {
		return Result{}, fmt.Errorf("send fixed SIGTERM: %w", err)
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, err := h.Linux.Process(ctx, envelope.TargetSnapshot.PID)
		if errors.Is(err, os.ErrNotExist) || (err == nil && info.State == "Z") {
			evidence := map[string]string{"pid": fmt.Sprint(envelope.TargetSnapshot.PID), "start_ticks": fmt.Sprint(envelope.TargetSnapshot.StartTicks), "executable": envelope.TargetSnapshot.Executable, "signal": "SIGTERM", "post_state": "not_running"}
			return Result{Tool: envelope.Proposal.Tool, Mode: LabSandbox, Status: "SUCCEEDED", Message: "allowlisted process terminated with SIGTERM and verified", ActionID: envelope.Proposal.ProposalID, Changed: true, Verification: &Verification{Verified: true, Checks: []string{"PID/start time/executable snapshot revalidated", "fixed SIGTERM sent", "process absent or zombie after signal"}, Evidence: evidence}, StartedAt: started, FinishedAt: time.Now().UTC()}, nil
		}
		if err != nil {
			return Result{}, fmt.Errorf("verify process termination: %w", err)
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-deadline.C:
			return Result{}, errors.New("process remained alive after fixed SIGTERM timeout; SIGKILL was not attempted")
		case <-ticker.C:
		}
	}
}
