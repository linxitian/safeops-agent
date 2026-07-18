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

func TestReaderRejectsSymlinksEvenWhenTargetRemainsInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.conf")
	link := filepath.Join(root, "alias.conf")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Metadata(context.Background(), link); err == nil {
		t.Fatal("metadata followed an allowlisted symlink")
	}
	if _, err := reader.Hash(context.Background(), link, 100); err == nil {
		t.Fatal("hash followed an allowlisted symlink")
	}
	snapshot, err := reader.Snapshot(context.Background(), root, 10, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].RelativePath != "target.conf" || len(snapshot.Skipped) != 1 || snapshot.Skipped[0].RelativePath != "alias.conf" || snapshot.Skipped[0].Reason != "symlink" {
		t.Fatalf("snapshot did not fail closed for symlink: %+v", snapshot)
	}
}

func TestReaderPinsAllowlistedRootIdentity(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "managed")
	displaced := filepath.Join(parent, "managed-old")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "value")
	if err := os.WriteFile(path, []byte("approved-root"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, displaced); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement-root"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := reader.Hash(context.Background(), path, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := "57abe0127e23c1858b57ad2fe8512134b4c8814eb6a7559cf72f48b2195c931e"
	if value.SHA256 != want {
		t.Fatalf("reader followed replacement root: %s", value.SHA256)
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

func TestReaderSnapshotsAllowlistedFileRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mcp_servers.yaml")
	if err := os.WriteFile(path, []byte("servers: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reader.Snapshot(context.Background(), path, 1, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Root != path || len(snapshot.Entries) != 1 || snapshot.Entries[0].RelativePath != "." || snapshot.Entries[0].SHA256 == "" {
		t.Fatalf("unexpected file snapshot: %+v", snapshot)
	}
}

func TestReaderRejectsBroadOrRelativeRoots(t *testing.T) {
	if _, err := NewReader("/"); err == nil {
		t.Fatal("filesystem root accepted")
	}
	if _, err := NewReader("relative"); err == nil {
		t.Fatal("relative root accepted")
	}
	if _, err := NewReader(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing root accepted")
	}
}
