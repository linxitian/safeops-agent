package system

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/platform"
)

func TestSystemServerProtocolAndTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := New(fakePlatform{})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "system-test", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
		if tool.InputSchema == nil {
			t.Fatalf("tool %s has no input schema", tool.Name)
		}
	}
	if len(names) != 8 {
		t.Fatalf("got %d tools, want 8: %v", len(names), names)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "system.get_memory_metrics", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result.Content)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok || structured["memory"] == nil {
		t.Fatalf("missing structured output: %#v", result.StructuredContent)
	}
}

type fakePlatform struct{}

func (fakePlatform) CPU(context.Context) (platform.CPUStat, error) {
	return platform.CPUStat{Total: 100, Busy: 25}, nil
}
func (fakePlatform) Memory(context.Context) (platform.MemoryStat, error) {
	return platform.MemoryStat{TotalBytes: 100, AvailableBytes: 40, UsedBytes: 60}, nil
}
func (fakePlatform) Load(context.Context) (platform.LoadAverage, error) {
	return platform.LoadAverage{One: .1}, nil
}
func (fakePlatform) Disk(context.Context, string) (platform.DiskUsage, error) {
	return platform.DiskUsage{Path: "/", TotalBytes: 100, UsedBytes: 20}, nil
}
func (fakePlatform) Mounts(context.Context) ([]platform.Mount, error) {
	return []platform.Mount{{Source: "/dev/vda", Target: "/"}}, nil
}
func (fakePlatform) Kernel(context.Context) (platform.KernelInfo, error) {
	return platform.KernelInfo{OS: "linux", Architecture: "loong64"}, nil
}
func (fakePlatform) Uptime(context.Context) (time.Duration, error) { return time.Hour, nil }
