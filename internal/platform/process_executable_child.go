package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ProcessExecutableHelperFlag is an internal, fixed-purpose mode supported by
// safeops-privexec. It is never populated from a model or MCP tool argument.
const ProcessExecutableHelperFlag = "internal-read-process-executable"

type processExecutableCommand struct {
	Binary string
	PID    int
	UID    uint32
	GID    uint32
}

type processExecutableCommandRunner interface {
	Run(context.Context, processExecutableCommand) ([]byte, error)
}

type osProcessExecutableCommandRunner struct{}

func (osProcessExecutableCommandRunner) Run(ctx context.Context, request processExecutableCommand) ([]byte, error) {
	stdout := &limitedBuffer{limit: 4096}
	stderr := &limitedBuffer{limit: 4096}
	cmd := newOSProcessExecutableCommand(ctx, request, stdout, stderr)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("fixed process executable helper failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func newOSProcessExecutableCommand(ctx context.Context, request processExecutableCommand, stdout, stderr io.Writer) *exec.Cmd {
	cmd := exec.CommandContext(ctx, request.Binary, "-"+ProcessExecutableHelperFlag, strconv.Itoa(request.PID))
	cmd.Env = []string{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: request.UID, Gid: request.GID, NoSetGroups: true},
		Pdeathsig:  syscall.SIGKILL,
	}
	return cmd
}

// NewProcessExecutableChildFallback constructs a fixed self-exec helper. The
// child drops to the already-observed target UID before reading /proc/PID/exe,
// which satisfies Linux's same-UID ptrace access check without CAP_SYS_PTRACE.
func NewProcessExecutableChildFallback(binary string) (ProcessExecutableFallback, error) {
	return newProcessExecutableChildFallback(binary, os.Getegid(), osProcessExecutableCommandRunner{})
}

func newProcessExecutableChildFallback(binary string, gid int, runner processExecutableCommandRunner) (ProcessExecutableFallback, error) {
	if !filepath.IsAbs(binary) || filepath.Clean(binary) != binary {
		return nil, errors.New("process executable helper binary must be an absolute clean path")
	}
	if gid < 0 || uint64(gid) > math.MaxUint32 {
		return nil, errors.New("process executable helper gid is invalid")
	}
	if runner == nil {
		return nil, errors.New("process executable helper runner is required")
	}
	return func(ctx context.Context, pid, uid int) (string, error) {
		if pid <= 0 {
			return "", errors.New("process executable helper pid must be positive")
		}
		if uid < 0 || uint64(uid) > math.MaxUint32 {
			return "", errors.New("process executable helper uid is invalid")
		}
		output, err := runner.Run(ctx, processExecutableCommand{Binary: binary, PID: pid, UID: uint32(uid), GID: uint32(gid)})
		if err != nil {
			return "", err
		}
		return validateProcessExecutable(string(output))
	}, nil
}

// ReadProcessExecutable is the helper-side fixed operation. It accepts only a
// validated PID and reads only the corresponding procfs executable symlink.
func ReadProcessExecutable(ctx context.Context, pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("pid must be positive")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	executable, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return "", fmt.Errorf("read process executable: %w", err)
	}
	return validateProcessExecutable(executable)
}
