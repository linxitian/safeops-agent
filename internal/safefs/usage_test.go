package safefs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUsageIsMetadataOnlyBoundedAndSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "top"), []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "nested"), []byte("123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "external")); err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := reader.Usage(context.Background(), root, 4, 100)
	if err != nil {
		t.Fatal(err)
	}
	if usage.SizeBytes != 10 || usage.Files != 2 || usage.Skipped != 1 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	bounded, err := reader.Usage(context.Background(), root, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bounded.Truncated {
		t.Fatalf("entry budget did not truncate: %+v", bounded)
	}
}
