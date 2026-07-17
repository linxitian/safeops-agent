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
	if len(scope.AllowedFileRoots) != 1 || len(scope.AllowedProcessExecutables) != 1 {
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
