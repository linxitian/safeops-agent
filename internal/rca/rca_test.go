package rca

import (
	"safeops-agent/internal/platform"
	"testing"
)

func TestConfidenceFormula(t *testing.T) {
	components := ConfidenceComponents{SignalMatch: 1, LogPatternMatch: 1, GraphConsistency: 1, CaseSimilarity: .5}
	if got := components.Score(); got != .95 {
		t.Fatalf("score=%v want .95", got)
	}
}
func TestPortConflictD1RequiresCompleteEvidence(t *testing.T) {
	input := PortConflictInput{Service: platform.ServiceStatus{Name: "safeops-demo-web.service", ActiveState: "failed"}, Port: 8080, Logs: []platform.JournalEvent{{Message: "listen failed: EADDRINUSE", Priority: 3}}, Sockets: []platform.SocketInfo{{LocalPort: 8080, Listening: true, Inode: "1"}}, Processes: []platform.ProcessInfo{{PID: 42, StartTicks: 99, Name: "safeops-port-holder"}}}
	output := DiagnosePortConflict(input)
	if output.RCA.DiagnosisLevel != D1 || output.RCA.RootCause == "" || output.RCA.Confidence != .9 || output.RCA.Culprit != "pid:42:start:99" {
		t.Fatalf("unexpected RCA: %+v", output.RCA)
	}
	if len(output.Graph.Nodes) < 4 || len(output.Graph.Edges) < 2 {
		t.Fatalf("unexpected graph: %+v", output.Graph)
	}
}
func TestPortConflictFallsBackWhenEvidenceMissing(t *testing.T) {
	output := DiagnosePortConflict(PortConflictInput{Service: platform.ServiceStatus{Name: "demo.service"}, Port: 8080})
	if output.RCA.DiagnosisLevel != D3 || output.RCA.RootCause != "" || len(output.RCA.MissingEvidence) != 3 {
		t.Fatalf("unexpected fallback: %+v", output.RCA)
	}
}
