package safefs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReaderEnforcesRootAndSymlinkBoundary(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.conf"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Hash(context.Background(), filepath.Join(root, "a.conf"), 100); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Resolve(filepath.Join(root, "escape")); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if _, err := reader.Resolve(filepath.Join(outside, "secret")); err == nil {
		t.Fatal("outside path accepted")
	}
}

func TestReaderBoundsAndSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "one.yaml"), []byte("value: one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.log"), make([]byte, 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, _ := NewReader(root)
	if _, err := reader.Hash(context.Background(), filepath.Join(root, "large.log"), 100); err == nil {
		t.Fatal("oversized hash accepted")
	}
	large, truncated, err := reader.FindLarge(context.Background(), root, 500, 2, 10)
	if err != nil || truncated || len(large) != 1 || large[0].Name != "large.log" {
		t.Fatalf("unexpected large files: %+v %v %v", large, truncated, err)
	}
	snapshot, err := reader.Snapshot(context.Background(), root, 10, 100, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || len(snapshot.Skipped) != 1 || snapshot.Entries[0].RelativePath != "one.yaml" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestReaderRejectsBroadOrRelativeRoots(t *testing.T) {
	if _, err := NewReader("/"); err == nil {
		t.Fatal("filesystem root accepted")
	}
	if _, err := NewReader("relative"); err == nil {
		t.Fatal("relative root accepted")
	}
}
