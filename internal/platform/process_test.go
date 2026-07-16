package platform

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProcessParsingSearchAndPortCorrelation(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	etc := filepath.Join(root, "etc")
	pidRoot := filepath.Join(proc, "123")
	stat := "123 (safeops worker) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 999 4096 20 0 0\n"
	mustWrite(t, filepath.Join(pidRoot, "stat"), stat)
	mustWrite(t, filepath.Join(pidRoot, "status"), "Name:\tsafeops worker\nUid:\t1000\t1000\t1000\t1000\n")
	mustWrite(t, filepath.Join(pidRoot, "cmdline"), "safeops\x00--token=secret-value\x00serve\x00")
	if err := os.Symlink("/opt/safeops/bin/worker", filepath.Join(pidRoot, "exe")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(proc, "net", "tcp"), "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 12345 1 0000000000000000\n")
	for _, name := range []string{"tcp6", "udp", "udp6"} {
		mustWrite(t, filepath.Join(proc, "net", name), "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n")
	}
	if err := os.MkdirAll(filepath.Join(pidRoot, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[12345]", filepath.Join(pidRoot, "fd", "4")); err != nil {
		t.Fatal(err)
	}
	p := NewLinux(WithRoots(proc, etc))
	info, err := p.Process(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "safeops worker" || info.PPID != 1 || info.StartTicks != 999 || info.CPUTicks != 23 || info.UID != 1000 {
		t.Fatalf("unexpected process: %+v", info)
	}
	if info.Command != "safeops --token=[REDACTED] serve" {
		t.Fatalf("command was not redacted: %q", info.Command)
	}
	results, err := p.Processes(context.Background(), ProcessQuery{Search: "WORKER", Limit: 10, SortBy: "pid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].PID != 123 {
		t.Fatalf("unexpected search: %+v", results)
	}
	owners, err := p.ProcessesByPort(context.Background(), 8080)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 1 || owners[0].StartTicks != 999 {
		t.Fatalf("unexpected port owners: %+v", owners)
	}
}

func TestParseProcessStatRejectsMalformed(t *testing.T) {
	if _, err := parseProcessStat("bad", 4096); err == nil {
		t.Fatal("malformed stat accepted")
	}
}
