package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/platform"
)

func TestFileCreateRequiresStringContentAndLeavesNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "created.txt")
	handler := FileCreateHandler{}
	for name, arguments := range map[string]map[string]any{
		"missing": {},
		"number":  {"content": 42},
	} {
		t.Run(name, func(t *testing.T) {
			envelope := contracts.ActionEnvelope{
				Proposal:       contracts.ActionProposal{Tool: "file.create", Arguments: arguments},
				TargetSnapshot: contracts.TargetSnapshot{Type: "file", CanonicalPath: path, ExpectAbsent: true},
			}
			if _, err := handler.Execute(context.Background(), envelope); err == nil {
				t.Fatal("invalid file content was accepted")
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid create left a file behind: %v", err)
			}
		})
	}
}

func TestServiceRestartUsesSnapshotUnitAndVerifies(t *testing.T) {
	runner := &handlerRunner{outputs: [][]byte{
		{},
		[]byte("Id=safeops-demo-web.service\nActiveState=active\nSubState=running\nMainPID=42\n"),
	}}
	commands := platform.NewCommandPlatformWithRunner(platform.CommandPaths{Systemctl: "/fixed/systemctl"}, runner)
	handler := ServiceRestartHandler{Commands: commands}
	envelope := contracts.ActionEnvelope{Proposal: contracts.ActionProposal{Tool: "service.restart", ProposalID: "proposal", Arguments: map[string]any{"unit": "sshd.service"}}, TargetSnapshot: contracts.TargetSnapshot{Type: "service", ServiceName: "safeops-demo-web.service"}}
	result, err := handler.Execute(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verification == nil || !result.Verification.Verified || len(runner.calls) != 2 {
		t.Fatalf("unexpected result/calls: %+v %+v", result, runner.calls)
	}
	first := runner.calls[0]
	if first[len(first)-1] != "safeops-demo-web.service" {
		t.Fatalf("model argument reached systemctl: %v", first)
	}
}

func TestProcessTerminateUsesOnlySIGTERMAndVerifies(t *testing.T) {
	reader := &sequenceProcessReader{states: []string{"R", "Z"}}
	var gotPID int
	var gotSignal syscall.Signal
	handler := ProcessTerminateHandler{Linux: reader, Signal: func(pid int, signal syscall.Signal) error { gotPID, gotSignal = pid, signal; return nil }, Timeout: time.Second}
	envelope := contracts.ActionEnvelope{Proposal: contracts.ActionProposal{Tool: "process.terminate", ProposalID: "proposal"}, TargetSnapshot: contracts.TargetSnapshot{Type: "process", PID: 4242, StartTicks: 99, Executable: "/opt/safeops/bin/safeops-port-holder"}}
	result, err := handler.Execute(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if gotPID != 4242 || gotSignal != syscall.SIGTERM || result.Verification == nil || !result.Verification.Verified {
		t.Fatalf("unexpected termination: pid=%d signal=%v result=%+v", gotPID, gotSignal, result)
	}
}

func TestProcessTerminateRefusesSelfAndNoSIGKILLFallback(t *testing.T) {
	handler := ProcessTerminateHandler{Linux: &sequenceProcessReader{states: []string{"R"}}, Signal: func(int, syscall.Signal) error { return nil }, Timeout: 2 * time.Millisecond}
	self := contracts.ActionEnvelope{Proposal: contracts.ActionProposal{Tool: "process.terminate"}, TargetSnapshot: contracts.TargetSnapshot{Type: "process", PID: os.Getpid()}}
	if _, err := handler.Execute(context.Background(), self); err == nil {
		t.Fatal("executor signaled itself")
	}
	other := contracts.ActionEnvelope{Proposal: contracts.ActionProposal{Tool: "process.terminate"}, TargetSnapshot: contracts.TargetSnapshot{Type: "process", PID: 4242}}
	if _, err := handler.Execute(context.Background(), other); err == nil {
		t.Fatal("non-terminating process was reported as stopped")
	}
}

type handlerRunner struct {
	outputs [][]byte
	calls   [][]string
}

func (r *handlerRunner) Run(_ context.Context, binary string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{binary}, args...))
	if len(r.outputs) == 0 {
		return nil, errors.New("unexpected call")
	}
	output := r.outputs[0]
	r.outputs = r.outputs[1:]
	return output, nil
}

type sequenceProcessReader struct {
	states []string
	index  int
}

func (r *sequenceProcessReader) Process(context.Context, int) (platform.ProcessInfo, error) {
	if len(r.states) == 0 {
		return platform.ProcessInfo{}, errors.New("no states")
	}
	index := r.index
	if index >= len(r.states) {
		index = len(r.states) - 1
	}
	r.index++
	return platform.ProcessInfo{State: r.states[index]}, nil
}
