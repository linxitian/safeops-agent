package platform

import (
	"context"
	"strings"
	"testing"
)

func TestCommandPlatformServiceUsesFixedStructuredArguments(t *testing.T) {
	runner := &fakeFixedRunner{output: []byte("Id=demo.service\nDescription=Demo Service\nLoadState=loaded\nActiveState=active\nSubState=running\nMainPID=42\nExecMainStatus=0\nNRestarts=3\nFragmentPath=/etc/systemd/system/demo.service\n")}
	p := NewCommandPlatformWithRunner(CommandPaths{Systemctl: "/fixed/systemctl"}, runner)
	status, err := p.Service(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if status.Name != "demo.service" || status.MainPID != 42 || status.RestartCount != 3 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if runner.binary != "/fixed/systemctl" || runner.args[len(runner.args)-1] != "demo.service" {
		t.Fatalf("unexpected invocation: %s %v", runner.binary, runner.args)
	}
}
func TestCommandPlatformRejectsUnitInjection(t *testing.T) {
	p := NewCommandPlatformWithRunner(CommandPaths{Systemctl: "systemctl"}, &fakeFixedRunner{})
	if _, err := p.Service(context.Background(), "demo; rm -rf /"); err == nil {
		t.Fatal("unit injection accepted")
	}
}
func TestFindFixedBinaryRejectsPathsAndMetacharacters(t *testing.T) {
	for _, name := range []string{"../bin/sh", "/bin/sh", "sh;id", "sh -c", ""} {
		if path := FindFixedBinary(name); path != "" {
			t.Fatalf("unsafe binary name %q resolved to %q", name, path)
		}
	}
}
func TestJournalParsingBoundsAndRedacts(t *testing.T) {
	line := `{"MESSAGE":"login token=top-secret failed","_SYSTEMD_UNIT":"demo.service","_PID":"42","PRIORITY":"3","_SOURCE_REALTIME_TIMESTAMP":"1000000","_BOOT_ID":"boot","__CURSOR":"cursor"}` + "\n"
	runner := &fakeFixedRunner{output: []byte(line)}
	p := NewCommandPlatformWithRunner(CommandPaths{Journalctl: "/fixed/journalctl"}, runner)
	events, err := p.Journal(context.Background(), JournalQuery{Unit: "demo", Lines: 20, Priority: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Message != "login token=[REDACTED] failed" || !events[0].Redacted {
		t.Fatalf("unexpected event: %+v", events)
	}
	joined := strings.Join(runner.args, " ")
	if !strings.Contains(joined, "--unit=demo.service") || !strings.Contains(joined, "--priority=3") {
		t.Fatalf("unexpected args: %v", runner.args)
	}
}
func TestJournalRejectsExcessiveLines(t *testing.T) {
	p := NewCommandPlatformWithRunner(CommandPaths{Journalctl: "journalctl"}, &fakeFixedRunner{})
	if _, err := p.Journal(context.Background(), JournalQuery{Lines: 501, Priority: -1}); err == nil {
		t.Fatal("excessive journal request accepted")
	}
}

type fakeFixedRunner struct {
	binary string
	args   []string
	output []byte
	err    error
}

func (r *fakeFixedRunner) Run(_ context.Context, binary string, args ...string) ([]byte, error) {
	r.binary = binary
	r.args = append([]string(nil), args...)
	return r.output, r.err
}
