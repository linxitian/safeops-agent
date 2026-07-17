package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

type recordingProcessExecutableRunner struct {
	request processExecutableCommand
	output  []byte
	err     error
}

func (r *recordingProcessExecutableRunner) Run(_ context.Context, request processExecutableCommand) ([]byte, error) {
	r.request = request
	return r.output, r.err
}

func TestResolveProcessExecutableUsesNarrowFallback(t *testing.T) {
	called := false
	fallback := func(_ context.Context, pid, uid int) (string, error) {
		called = true
		if pid != 42 || uid != 1000 {
			t.Fatalf("fallback identity = pid %d uid %d", pid, uid)
		}
		return "/opt/safeops/bin/safeops-port-holder", nil
	}
	executable, err := resolveProcessExecutable(context.Background(), "/proc/42/exe", 42, 1000, func(string) (string, error) {
		return "", os.ErrPermission
	}, fallback)
	if err != nil {
		t.Fatal(err)
	}
	if !called || executable != "/opt/safeops/bin/safeops-port-holder" {
		t.Fatalf("unexpected fallback result: called=%t executable=%q", called, executable)
	}
}

func TestResolveProcessExecutableFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		readErr  error
		fallback ProcessExecutableFallback
	}{
		{name: "no fallback", readErr: os.ErrPermission},
		{name: "non-permission error", readErr: os.ErrNotExist, fallback: func(context.Context, int, int) (string, error) { return "/tmp/should-not-run", nil }},
		{name: "fallback error", readErr: os.ErrPermission, fallback: func(context.Context, int, int) (string, error) { return "", errors.New("helper denied") }},
		{name: "invalid fallback output", readErr: os.ErrPermission, fallback: func(context.Context, int, int) (string, error) { return "/tmp/a\x00/tmp/b", nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveProcessExecutable(context.Background(), "/proc/42/exe", 42, 1000, func(string) (string, error) {
				return "", test.readErr
			}, test.fallback)
			if err == nil {
				t.Fatal("unsafe executable identity was accepted")
			}
		})
	}
}

func TestProcessExecutableChildFallbackUsesFixedSelfCommand(t *testing.T) {
	runner := &recordingProcessExecutableRunner{output: []byte("/opt/safeops/bin/safeops-port-holder")}
	fallback, err := newProcessExecutableChildFallback("/opt/safeops/bin/safeops-privexec", 981, runner)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := fallback(context.Background(), 42, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if executable != "/opt/safeops/bin/safeops-port-holder" {
		t.Fatalf("executable = %q", executable)
	}
	want := processExecutableCommand{Binary: "/opt/safeops/bin/safeops-privexec", PID: 42, UID: 1000, GID: 981}
	if runner.request != want {
		t.Fatalf("helper request = %+v, want %+v", runner.request, want)
	}
}

func TestOSProcessExecutableCommandHasFixedArgumentsAndDroppedCredentials(t *testing.T) {
	request := processExecutableCommand{Binary: "/opt/safeops/bin/safeops-privexec", PID: 42, UID: 1000, GID: 981}
	cmd := newOSProcessExecutableCommand(context.Background(), request, io.Discard, io.Discard)
	wantArgs := []string{"/opt/safeops/bin/safeops-privexec", "-" + ProcessExecutableHelperFlag, "42"}
	if strings.Join(cmd.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("helper args = %q, want %q", cmd.Args, wantArgs)
	}
	if cmd.Env == nil || len(cmd.Env) != 0 {
		t.Fatalf("helper inherited environment: %#v", cmd.Env)
	}
	credential := cmd.SysProcAttr.Credential
	if credential == nil || credential.Uid != 1000 || credential.Gid != 981 || !credential.NoSetGroups {
		t.Fatalf("helper credential = %+v", credential)
	}
	if cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("helper parent-death signal = %d", cmd.SysProcAttr.Pdeathsig)
	}
}

func TestProcessExecutableChildFallbackRejectsInvalidInputs(t *testing.T) {
	runner := &recordingProcessExecutableRunner{output: []byte("/tmp/process")}
	if _, err := newProcessExecutableChildFallback("safeops-privexec", 981, runner); err == nil {
		t.Fatal("relative helper binary accepted")
	}
	fallback, err := newProcessExecutableChildFallback("/opt/safeops/bin/safeops-privexec", 981, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fallback(context.Background(), 0, 1000); err == nil {
		t.Fatal("invalid pid accepted")
	}
	if _, err := fallback(context.Background(), 42, -1); err == nil {
		t.Fatal("invalid uid accepted")
	}
	runner.err = errors.New("start denied")
	if _, err := fallback(context.Background(), 42, 1000); err == nil {
		t.Fatal("helper start failure accepted")
	}
}

func TestReadProcessExecutableCurrentProcess(t *testing.T) {
	executable, err := ReadProcessExecutable(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if executable == "" {
		t.Fatal("current executable was empty")
	}
}

func TestPrivexecUnitUsesSetUIDCapabilityWithoutPtrace(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "deploy", "systemd", "safeops-privexec.service"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "CapabilityBoundingSet=CAP_KILL CAP_DAC_OVERRIDE CAP_FOWNER CAP_SETUID") {
		t.Fatal("privexec unit does not include the scoped child-uid capability")
	}
	if !strings.Contains(text, "AmbientCapabilities=CAP_SETUID") {
		t.Fatal("privexec unit does not grant the scoped child-uid capability")
	}
	if strings.Contains(text, "CAP_SYS_PTRACE") {
		t.Fatal("privexec unit must not gain broad process tracing capability")
	}
}
