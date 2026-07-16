package rollback

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"safeops-agent/contracts"
)

func TestQuarantineAndRestoreAreVerifiedAndReversible(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "lab")
	quarantine := filepath.Join(root, "quarantine")
	if err := os.MkdirAll(lab, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lab, "large.log")
	if err := os.WriteFile(path, []byte("controlled lab data"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewQuarantineManager([]string{lab}, quarantine)
	if err != nil {
		t.Fatal(err)
	}
	original := fileSnapshot(t, "file:large", path)
	operation, err := manager.Quarantine(context.Background(), "task_1", "nonce_1", original)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Manifest.Status != Committed {
		t.Fatalf("unexpected manifest: %+v", operation.Manifest)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("original file still exists")
	}
	if _, err := os.Stat(operation.Manifest.QuarantinedPath); err != nil {
		t.Fatal(err)
	}
	quarantined := fileSnapshot(t, "file:large", operation.Manifest.QuarantinedPath)
	restored, err := manager.Restore(context.Background(), operation.Manifest.ID, quarantined)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Manifest.Status != Restored {
		t.Fatalf("unexpected restored manifest: %+v", restored.Manifest)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "controlled lab data" {
		t.Fatalf("file was not restored: %q %v", content, err)
	}
}

func TestQuarantineRejectsOutsideAndChangedTargets(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "lab")
	if err := os.MkdirAll(lab, 0o750); err != nil {
		t.Fatal(err)
	}
	manager, err := NewQuarantineManager([]string{lab}, filepath.Join(root, "quarantine"))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.log")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Quarantine(context.Background(), "task", "nonce", fileSnapshot(t, "outside", outside)); err == nil {
		t.Fatal("outside file was quarantined")
	}
	inside := filepath.Join(lab, "inside.log")
	if err := os.WriteFile(inside, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := fileSnapshot(t, "inside", inside)
	if err := os.WriteFile(inside, []byte("changed and longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Quarantine(context.Background(), "task", "nonce", expected); err == nil {
		t.Fatal("changed file was quarantined")
	}
}

func TestRestoreRefusesOccupiedOriginalPath(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "lab")
	_ = os.MkdirAll(lab, 0o750)
	path := filepath.Join(lab, "demo.log")
	_ = os.WriteFile(path, []byte("original"), 0o600)
	manager, _ := NewQuarantineManager([]string{lab}, filepath.Join(root, "quarantine"))
	operation, err := manager.Quarantine(context.Background(), "task", "nonce", fileSnapshot(t, "demo", path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	quarantined := fileSnapshot(t, "demo", operation.Manifest.QuarantinedPath)
	if _, err := manager.Restore(context.Background(), operation.Manifest.ID, quarantined); err == nil {
		t.Fatal("restore overwrote an occupied original path")
	}
}

func fileSnapshot(t *testing.T, id, path string) contracts.TargetSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("inode unavailable")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return contracts.TargetSnapshot{Type: "file", ID: id, CanonicalPath: canonical, Size: info.Size(), MTimeUnixNano: info.ModTime().UnixNano(), Mode: uint32(info.Mode()), Inode: stat.Ino}
}
