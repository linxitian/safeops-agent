package executor

import (
	"path/filepath"
	"testing"

	"safeops-agent/contracts"
)

func TestFixedScopeAuthorizesOnlyAllowlistedTargets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lab")
	scope := FixedScope{
		AllowedServices:           map[string]bool{"safeops-demo-web.service": true},
		AllowedFileRoots:          []string{root},
		AllowedProcessExecutables: []string{"/opt/safeops/bin"},
	}
	allowed := []contracts.ActionEnvelope{
		{TargetSnapshot: contracts.TargetSnapshot{Type: "service", ServiceName: "SAFEOPS-DEMO-WEB.SERVICE"}},
		{TargetSnapshot: contracts.TargetSnapshot{Type: "file", CanonicalPath: filepath.Join(root, "logs", "app.log")}},
		{TargetSnapshot: contracts.TargetSnapshot{Type: "process", Executable: "/opt/safeops/bin/safeops-cpu-hog"}},
	}
	for _, envelope := range allowed {
		if err := scope.Authorize(envelope); err != nil {
			t.Fatalf("allowlisted target was denied: %+v: %v", envelope.TargetSnapshot, err)
		}
	}
	denied := []contracts.ActionEnvelope{
		{TargetSnapshot: contracts.TargetSnapshot{Type: "service", ServiceName: "sshd.service"}},
		{TargetSnapshot: contracts.TargetSnapshot{Type: "file", CanonicalPath: root + "-evil/app.log"}},
		{TargetSnapshot: contracts.TargetSnapshot{Type: "process", Executable: "/usr/bin/bash"}},
		{TargetSnapshot: contracts.TargetSnapshot{Type: "host", ID: "local"}},
	}
	for _, envelope := range denied {
		if err := scope.Authorize(envelope); err == nil {
			t.Fatalf("non-allowlisted target was accepted: %+v", envelope.TargetSnapshot)
		}
	}
}

func TestWithinRootsDoesNotAcceptSiblingPrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lab")
	if withinRoots(root+"-backup/file.log", []string{root}) {
		t.Fatal("sibling path with common prefix was accepted")
	}
	if !withinRoots(filepath.Join(root, "file.log"), []string{root}) {
		t.Fatal("child path under root was denied")
	}
}
