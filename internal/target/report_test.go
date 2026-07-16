package target

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeAndReportAreTruthfulAndChecksummed(t *testing.T) {
	report := Probe(context.Background())
	if report.ReportID == "" || report.EvidenceLevel != "NATIVE_EXECUTION_REPORT" || report.TargetVerified {
		t.Fatalf("unexpected report identity: %+v", report)
	}
	directory := t.TempDir()
	jsonPath, markdownPath, err := Write(report, directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{jsonPath, markdownPath, jsonPath + ".sha256"} {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("missing report artifact %s: %v", path, err)
		}
	}
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var reopened Report
	if err := json.Unmarshal(b, &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.TargetVerified || filepath.Dir(jsonPath) != directory {
		t.Fatalf("report made an unsupported target claim: %+v", reopened)
	}
	if filepath.Base(jsonPath) != "target-probe-report.json" || filepath.Base(markdownPath) != "target-probe-report.txt" {
		t.Fatalf("unexpected required artifact names: %s %s", jsonPath, markdownPath)
	}
	wanted := map[string]bool{
		"kernel_info": false, "goarch_loong64": false, "glibc_version": false, "systemd_version": false,
		"command_journalctl": false, "command_systemctl": false, "command_ss": false, "command_ip": false,
		"command_git": false, "command_go": false, "command_gcc": false, "proc_filesystem": false,
		"proc_memory": false, "root_statfs": false, "journalctl_json_support": false,
	}
	for _, check := range report.Checks {
		if _, exists := wanted[check.Name]; exists {
			wanted[check.Name] = true
		}
	}
	for name, found := range wanted {
		if !found {
			t.Fatalf("required target probe check missing: %s", name)
		}
	}
}
