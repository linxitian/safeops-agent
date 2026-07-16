package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"safeops-agent/contracts"
	"safeops-agent/internal/platform"
	"strings"
	"syscall"
)

type LinuxTargets struct {
	Linux            *platform.LinuxPlatform
	Commands         *platform.CommandPlatform
	AllowedFileRoots []string
}

func (t LinuxTargets) SnapshotProcess(ctx context.Context, targetID string, pid int) (contracts.TargetSnapshot, error) {
	identity, err := t.Linux.ProcessIdentity(ctx, pid)
	if err != nil {
		return contracts.TargetSnapshot{}, err
	}
	return contracts.TargetSnapshot{Type: "process", ID: targetID, PID: pid, StartTicks: identity.StartTicks, CommandDigest: identity.CommandDigest, UID: identity.UID, Executable: identity.Executable}, nil
}
func (t LinuxTargets) SnapshotService(ctx context.Context, targetID, unit string) (contracts.TargetSnapshot, error) {
	status, err := t.Commands.Service(ctx, unit)
	if err != nil {
		return contracts.TargetSnapshot{}, err
	}
	return contracts.TargetSnapshot{Type: "service", ID: targetID, ServiceName: status.Name, ActiveState: status.ActiveState, MainPID: status.MainPID}, nil
}
func (t LinuxTargets) SnapshotFile(_ context.Context, targetID, path string) (contracts.TargetSnapshot, error) {
	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return contracts.TargetSnapshot{}, err
	}
	if !withinRoots(canonical, t.AllowedFileRoots) {
		return contracts.TargetSnapshot{}, errors.New("file path is outside allowed roots")
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return contracts.TargetSnapshot{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return contracts.TargetSnapshot{}, errors.New("file inode unavailable")
	}
	return contracts.TargetSnapshot{Type: "file", ID: targetID, CanonicalPath: canonical, Size: info.Size(), MTimeUnixNano: info.ModTime().UnixNano(), Mode: uint32(info.Mode()), Inode: stat.Ino}, nil
}
func (t LinuxTargets) Revalidate(ctx context.Context, expected contracts.TargetSnapshot) error {
	var current contracts.TargetSnapshot
	var err error
	switch expected.Type {
	case "process":
		current, err = t.SnapshotProcess(ctx, expected.ID, expected.PID)
	case "service":
		current, err = t.SnapshotService(ctx, expected.ID, expected.ServiceName)
	case "file":
		current, err = t.SnapshotFile(ctx, expected.ID, expected.CanonicalPath)
	default:
		return errors.New("unsupported target snapshot type")
	}
	if err != nil {
		return err
	}
	expectedDigest, _ := expected.Digest()
	currentDigest, _ := current.Digest()
	if expectedDigest != currentDigest {
		return fmt.Errorf("snapshot digest changed: expected %s got %s", expectedDigest, currentDigest)
	}
	return nil
}

type FixedScope struct {
	AllowedServices           map[string]bool
	AllowedFileRoots          []string
	AllowedProcessExecutables []string
}

func (s FixedScope) Authorize(envelope contracts.ActionEnvelope) error {
	snapshot := envelope.TargetSnapshot
	switch snapshot.Type {
	case "service":
		if !s.AllowedServices[strings.ToLower(snapshot.ServiceName)] {
			return errors.New("service is not allowlisted")
		}
	case "file":
		if !withinRoots(snapshot.CanonicalPath, s.AllowedFileRoots) {
			return errors.New("file path is not allowlisted")
		}
	case "process":
		allowed := false
		for _, prefix := range s.AllowedProcessExecutables {
			if snapshot.Executable == prefix || strings.HasPrefix(snapshot.Executable, prefix+string(filepath.Separator)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return errors.New("process executable is not allowlisted")
		}
	default:
		return errors.New("unsupported target type")
	}
	return nil
}
func withinRoots(path string, roots []string) bool {
	clean := filepath.Clean(path)
	for _, root := range roots {
		root = filepath.Clean(root)
		relative, err := filepath.Rel(root, clean)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
