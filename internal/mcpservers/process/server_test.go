package process

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"safeops-agent/internal/platform"
)

func TestProcessToolsOverMCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go New(fakeProcess{}).Run(ctx, serverTransport)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	count := 0
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		count++
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("tool %s missing schema", tool.Name)
		}
	}
	if count != 5 {
		t.Fatalf("got %d tools, want 5", count)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "process.find_by_port", Arguments: map[string]any{"port": 8080}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.StructuredContent == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "process.get_details", Arguments: map[string]any{"pid": -1}})
	if err != nil {
		t.Fatal(err)
	}
	if !invalid.IsError {
		t.Fatal("invalid pid was accepted")
	}
}

type fakeProcess struct{}

func (fakeProcess) Processes(context.Context, platform.ProcessQuery) ([]platform.ProcessInfo, error) {
	return []platform.ProcessInfo{{PID: 7, StartTicks: 99}}, nil
}
func (fakeProcess) Process(context.Context, int) (platform.ProcessInfo, error) {
	return platform.ProcessInfo{PID: 7, StartTicks: 99}, nil
}
func (fakeProcess) ProcessesByPort(context.Context, int) ([]platform.ProcessInfo, error) {
	return []platform.ProcessInfo{{PID: 7, StartTicks: 99}}, nil
}
