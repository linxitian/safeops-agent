package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSecretFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret, err := ReadSecretFile(path)
	if err != nil || len(secret) != 32 {
		t.Fatalf("valid secret rejected: %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretFile(path); err == nil {
		t.Fatal("group-readable secret accepted")
	}
}
