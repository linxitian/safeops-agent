package platform

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLinuxPlatformProcParsers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	etc := filepath.Join(root, "etc")
	mustWrite(t, filepath.Join(proc, "stat"), "cpu  10 2 3 100 5 1 2 4 0 0\n")
	mustWrite(t, filepath.Join(proc, "meminfo"), "MemTotal:       1000 kB\nMemFree:         100 kB\nMemAvailable:    400 kB\nBuffers:          20 kB\nCached:           30 kB\nSwapTotal:       200 kB\nSwapFree:        150 kB\n")
	mustWrite(t, filepath.Join(proc, "loadavg"), "0.10 0.20 0.30 2/100 4321\n")
	mustWrite(t, filepath.Join(proc, "uptime"), "12.50 2.00\n")
	mustWrite(t, filepath.Join(proc, "self", "mounts"), "/dev/vda1 / ext4 rw,relatime 0 0\ntmpfs /run tmpfs rw,nosuid 0 0\n")
	mustWrite(t, filepath.Join(etc, "os-release"), "PRETTY_NAME=\"Test Linux\"\nVERSION_ID=\"11\"\n")

	p := NewLinux(WithRoots(proc, etc))
	p.now = func() time.Time { return time.Unix(100, 0) }
	ctx := context.Background()
	cpu, err := p.CPU(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cpu.Total != 127 || cpu.Busy != 22 {
		t.Fatalf("unexpected CPU: %+v", cpu)
	}
	mem, err := p.Memory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mem.TotalBytes != 1000*1024 || mem.UsedBytes != 600*1024 {
		t.Fatalf("unexpected memory: %+v", mem)
	}
	load, err := p.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if load.One != 0.10 || load.RunningProcesses != 2 || load.LastPID != 4321 {
		t.Fatalf("unexpected load: %+v", load)
	}
	uptime, err := p.Uptime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if uptime != 12500*time.Millisecond {
		t.Fatalf("unexpected uptime: %s", uptime)
	}
	mounts, err := p.Mounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[0].Target != "/" {
		t.Fatalf("unexpected mounts: %+v", mounts)
	}
	kernel, err := p.Kernel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kernel.OSName != "Test Linux" || kernel.OSVersion != "11" {
		t.Fatalf("unexpected kernel info: %+v", kernel)
	}
}

func TestLinuxPlatformReadsRealProc(t *testing.T) {
	if _, err := os.Stat("/proc/stat"); err != nil {
		t.Skip("Linux procfs unavailable")
	}
	p := NewLinux()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := p.CPU(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Memory(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Disk(ctx, "/"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Mounts(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Kernel(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Uptime(ctx); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
