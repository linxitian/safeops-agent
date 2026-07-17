package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRejectsInvalidManifests(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate": `
servers:
  - id: system
    name: one
    transport: stdio
    command: ./bin/mcp-system
  - id: system
    name: two
    transport: stdio
    command: ./bin/mcp-system
`,
		"transport": `
servers:
  - id: system
    name: one
    transport: http
    command: ./bin/mcp-system
`,
		"command": `
servers:
  - id: system
    name: one
    transport: stdio
`,
		"name": `
servers:
  - id: system
    transport: stdio
    command: ./bin/mcp-system
`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp.yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid manifest was accepted")
			}
		})
	}
}

func TestLoadAcceptsMinimalStdioManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.yaml")
	if err := os.WriteFile(path, []byte(`
servers:
  - id: system
    name: safeops-mcp-system
    transport: stdio
    command: ./bin/mcp-system
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Servers) != 1 || config.Servers[0].ID != "system" {
		t.Fatalf("unexpected manifest: %+v", config)
	}
}
