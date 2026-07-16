package registry_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/mcpservers/system"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/registry"
)

const runServerEnv = "SAFEOPS_REGISTRY_TEST_SERVER"
const dynamicToolFileEnv = "SAFEOPS_REGISTRY_TEST_TOOL_FILE"

type emptyInput struct{}
type statusOutput struct {
	Status string `json:"status"`
}

func TestMain(m *testing.M) {
	if os.Getenv(runServerEnv) == "1" {
		if path := os.Getenv(dynamicToolFileEnv); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				os.Exit(3)
			}
			s := mcp.NewServer(&mcp.Implementation{Name: "dynamic-test", Version: "1"}, nil)
			mcp.AddTool(s, &mcp.Tool{Name: "test.one", Description: "one"}, func(context.Context, *mcp.CallToolRequest, emptyInput) (*mcp.CallToolResult, statusOutput, error) {
				return &mcp.CallToolResult{}, statusOutput{Status: "one"}, nil
			})
			if string(b) == "two" {
				mcp.AddTool(s, &mcp.Tool{Name: "test.two", Description: "two"}, func(context.Context, *mcp.CallToolRequest, emptyInput) (*mcp.CallToolResult, statusOutput, error) {
					return &mcp.CallToolResult{}, statusOutput{Status: "two"}, nil
				})
			}
			if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
				os.Exit(2)
			}
			return
		}
		if err := system.New(platform.NewLinux()).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			os.Exit(2)
		}
		return
	}
	os.Exit(m.Run())
}

func TestRegistryLifecycleAndToolListChange(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toolFile := filepath.Join(t.TempDir(), "tools")
	if err := os.WriteFile(toolFile, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := registry.Config{Servers: []registry.ServerManifest{{ID: "dynamic", Name: "dynamic", Version: "1", Transport: "stdio", Command: executable, Enabled: true, Environment: map[string]string{runServerEnv: "1", dynamicToolFileEnv: toolFile}}}}
	r := registry.New(cfg)
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	first := r.States()[0]
	if len(first.Tools) != 1 || first.ToolSetHash == "" || first.ToolsChanged {
		t.Fatalf("unexpected first discovery: %+v", first)
	}
	if err := os.WriteFile(toolFile, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.Discover(ctx, "dynamic"); err != nil {
		t.Fatal(err)
	}
	changed := r.States()[0]
	if len(changed.Tools) != 2 || !changed.ToolsChanged || changed.PreviousToolSetHash != first.ToolSetHash || changed.ToolSetHash == first.ToolSetHash {
		t.Fatalf("tool-list change not detected: %+v", changed)
	}
	if err := r.Discover(ctx, "dynamic"); err != nil {
		t.Fatal(err)
	}
	if r.States()[0].ToolsChanged {
		t.Fatal("unchanged rediscovery reported a tool-list change")
	}
	if err := r.SetEnabled(ctx, "dynamic", false); err != nil {
		t.Fatal(err)
	}
	disabled := r.States()[0]
	if disabled.Status != registry.StatusDisabled || disabled.Manifest.Enabled || len(disabled.Tools) != 0 {
		t.Fatalf("unexpected disabled state: %+v", disabled)
	}
	if _, err := r.CallTool(ctx, "dynamic", "test.one", map[string]any{}); err == nil {
		t.Fatal("disabled server remained callable")
	}
	if err := r.SetEnabled(ctx, "dynamic", true); err != nil {
		t.Fatal(err)
	}
	enabled := r.States()[0]
	if enabled.Status != registry.StatusHealthy || !enabled.Manifest.Enabled || len(enabled.Tools) != 2 {
		t.Fatalf("unexpected re-enabled state: %+v", enabled)
	}
}

func TestRegistryDiscoversAndCallsStdioMCP(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := registry.Config{Servers: []registry.ServerManifest{{ID: "system", Name: "test-system", Version: system.Version, Transport: "stdio", Command: executable, Enabled: true, Environment: map[string]string{runServerEnv: "1"}}}}
	r := registry.New(cfg)
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	states := r.States()
	if len(states) != 1 || states[0].Status != registry.StatusHealthy || len(states[0].Tools) != 8 {
		t.Fatalf("unexpected registry state: %+v", states)
	}
	result, err := r.CallTool(ctx, "system", "system.get_cpu_metrics", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.StructuredContent == nil {
		t.Fatalf("unexpected call result: %+v", result)
	}
	if err := r.Health(ctx, "system"); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryRejectsUnknownTool(t *testing.T) {
	r := registry.New(registry.Config{})
	if _, err := r.CallTool(context.Background(), "missing", "shell.execute", map[string]any{}); err == nil {
		t.Fatal("unknown tool was accepted")
	}
}

func TestDeploymentManifestUsesAbsoluteCommands(t *testing.T) {
	config, err := registry.Load(filepath.Join("..", "..", "deploy", "config", "mcp_servers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Servers) != 8 {
		t.Fatalf("deployment manifest has %d servers, want 8", len(config.Servers))
	}
	for _, server := range config.Servers {
		if !filepath.IsAbs(server.Command) || filepath.Dir(server.Command) != "/opt/safeops/bin" {
			t.Fatalf("server %s command is not an absolute release path: %q", server.ID, server.Command)
		}
	}
}
