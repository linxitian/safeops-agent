package platform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedSysctlsUseFixedProcPathsAndBounds(t *testing.T) {
	proc := t.TempDir()
	etc := t.TempDir()
	path := filepath.Join(proc, "sys", "kernel")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "pid_max"), []byte("4194304\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewLinux(WithRoots(proc, etc))
	settings, err := p.Sysctls(context.Background(), []string{"kernel.pid_max", "kernel.pid_max"})
	if err != nil || len(settings) != 1 || settings[0].Value != "4194304" {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
	for _, invalid := range []string{"../etc/passwd", "kernel.pid_max/../../secret", "kernel"} {
		if _, err := p.Sysctls(context.Background(), []string{invalid}); err == nil {
			t.Fatalf("accepted invalid sysctl key %q", invalid)
		}
	}
	if err := os.WriteFile(filepath.Join(path, "too_large"), []byte(strings.Repeat("x", 4097)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Sysctls(context.Background(), []string{"kernel.too_large"}); err == nil {
		t.Fatal("accepted overlong sysctl value")
	}
}
