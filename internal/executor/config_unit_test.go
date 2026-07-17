package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigNormalizesServicesAndBuildsScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executor.yaml")
	if err := os.WriteFile(path, []byte(`
schema_version: 1
allowed_services:
  - " SafeOps-Demo-Web.Service "
allowed_file_roots:
  - /var/lib/safeops/lab
  - /var/lib/safeops/quarantine
lab_file_roots:
  - /var/lib/safeops/lab
quarantine_root: /var/lib/safeops/quarantine
allowed_process_executables:
  - /opt/safeops/bin
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.AllowedServices[0] != "safeops-demo-web.service" {
		t.Fatalf("service was not normalized: %+v", config.AllowedServices)
	}
	scope := config.Scope()
	if !scope.AllowedServices["safeops-demo-web.service"] {
		t.Fatalf("scope omitted normalized service: %+v", scope.AllowedServices)
	}
	if len(scope.AllowedFileRoots) != 2 || len(scope.AllowedProcessExecutables) != 1 {
		t.Fatalf("scope lost allowlists: %+v", scope)
	}
}

func TestLoadConfigRejectsUnsupportedSchemaAndMissingLabRoots(t *testing.T) {
	for name, body := range map[string]string{
		"schema": `
schema_version: 2
lab_file_roots: [/var/lib/safeops/lab]
quarantine_root: /var/lib/safeops/quarantine
`,
		"lab-roots": `
schema_version: 1
quarantine_root: /var/lib/safeops/quarantine
`,
		"quarantine": `
schema_version: 1
lab_file_roots: [/var/lib/safeops/lab]
`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "executor.yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil {
				t.Fatal("invalid executor config was accepted")
			}
		})
	}
}

func TestNormalizeAllowlistRootsRejectsQuarantineOverlapInBothDirections(t *testing.T) {
	root := t.TempDir()
	quarantine := filepath.Join(root, "quarantine")
	for name, managed := range map[string]string{
		"inside":   filepath.Join(quarantine, "nested"),
		"ancestor": root,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeAllowlistRoots([]string{managed}, quarantine); err == nil {
				t.Fatalf("overlapping managed root %s was accepted", managed)
			}
		})
	}
}

func TestNormalizeAllowlistRootRejectsSymlinkToRoot(t *testing.T) {
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(string(filepath.Separator), link); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeAllowlistRoot(link); err == nil {
		t.Fatal("symlink resolving to the root filesystem was accepted")
	}
}

func TestConfigManagerReloadsNarrowedRootsWithoutChangingMaximum(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "lab")
	narrowed := filepath.Join(lab, "config")
	quarantine := filepath.Join(root, "quarantine")
	for _, directory := range []string{narrowed, quarantine} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	statePath := filepath.Join(root, "state", "executor_allowlist.yaml")
	config := Config{LabFileRoots: []string{lab}, AllowedFileRoots: []string{lab, quarantine}, QuarantineRoot: quarantine}
	manager, err := NewConfigManager(statePath, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpdateManagedRoots([]string{narrowed}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewConfigManager(statePath, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	status := reloaded.Status()
	if len(status.ManagedRoots) != 1 || status.ManagedRoots[0] != narrowed || status.RequiresExecutorRestart {
		t.Fatalf("unexpected reloaded status: %+v", status)
	}
	if _, err := reloaded.UpdateManagedRoots([]string{root}); err == nil {
		t.Fatal("persisted narrowing was allowed to expand beyond the administrator maximum")
	}
}
