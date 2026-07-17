package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/platform"
)

const maxCreatedFileBytes = 64 << 10

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

type FileCreateHandler struct{}

func (FileCreateHandler) Execute(_ context.Context, envelope contracts.ActionEnvelope) (Result, error) {
	if envelope.Proposal.Tool != "file.create" || envelope.TargetSnapshot.Type != "file" || !envelope.TargetSnapshot.ExpectAbsent || envelope.TargetSnapshot.CanonicalPath == "" {
		return Result{}, errors.New("invalid file create envelope")
	}
	content, ok := envelope.Proposal.Arguments["content"].(string)
	if !ok {
		return Result{}, errors.New("file.create content must be a string")
	}
	if len([]byte(content)) > maxCreatedFileBytes {
		return Result{}, errors.New("file.create content exceeds bounded size limit")
	}
	started := time.Now().UTC()
	path := envelope.TargetSnapshot.CanonicalPath
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("create approved file: %w", err)
	}
	fail := func(cause error) (Result, error) {
		if file != nil {
			if err := file.Close(); err != nil {
				cause = errors.Join(cause, fmt.Errorf("close failed create: %w", err))
			}
			file = nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cause = errors.Join(cause, fmt.Errorf("remove failed create: %w", err))
		}
		if err := syncDirectory(filepath.Dir(path)); err != nil {
			cause = errors.Join(cause, fmt.Errorf("sync rollback parent: %w", err))
		}
		return Result{}, cause
	}
	if written, err := file.WriteString(content); err != nil {
		return fail(fmt.Errorf("write approved file: %w", err))
	} else if written != len(content) {
		return fail(fmt.Errorf("write approved file: %w", errors.New("short write")))
	}
	if err := file.Sync(); err != nil {
		return fail(fmt.Errorf("sync approved file: %w", err))
	}
	if err := file.Close(); err != nil {
		file = nil
		return fail(fmt.Errorf("close approved file: %w", err))
	}
	file = nil
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return fail(fmt.Errorf("sync file parent: %w", err))
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fail(fmt.Errorf("verify created file: %w", err))
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len([]byte(content))) || info.Mode().Perm()&0o077 != 0 {
		return fail(fmt.Errorf("created file verification failed: mode=%s size=%d", info.Mode(), info.Size()))
	}
	evidence := map[string]string{"path": path, "bytes": fmt.Sprint(info.Size()), "mode": info.Mode().String(), "rollback_strategy": envelope.Proposal.RollbackStrategy}
	return Result{Tool: envelope.Proposal.Tool, Mode: LabSandbox, Status: "SUCCEEDED", Message: "allowlisted file created and verified", ActionID: envelope.Proposal.ProposalID, Changed: true, Verification: &Verification{Verified: true, Checks: []string{"target absence and parent snapshot revalidated", "O_EXCL create used fixed 0600 mode", "created file metadata verified"}, Evidence: evidence}, StartedAt: started, FinishedAt: time.Now().UTC()}, nil
}

func syncDirectory(directory string) error {
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}
