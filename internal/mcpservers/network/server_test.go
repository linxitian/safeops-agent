package network

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"safeops-agent/internal/platform"
)

func TestNetworkToolsOverMCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, ct := mcp.NewInMemoryTransports()
	go New(fakeNetwork{}).Run(ctx, st)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
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
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "network.check_port", Arguments: map[string]any{"port": 8080}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %+v", result.Content)
	}
	invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "network.check_port", Arguments: map[string]any{"port": 70000}})
	if err != nil {
		t.Fatal(err)
	}
	if !invalid.IsError {
		t.Fatal("invalid port was accepted")
	}
}

type fakeNetwork struct{}

func (fakeNetwork) Sockets(context.Context, bool, int) ([]platform.SocketInfo, error) {
	return []platform.SocketInfo{{Protocol: "tcp", LocalPort: 8080, Listening: true}}, nil
}
func (fakeNetwork) Interfaces(context.Context) ([]platform.InterfaceInfo, error) {
	return []platform.InterfaceInfo{{Name: "lo", Index: 1}}, nil
}
func (fakeNetwork) ProcessesByPort(context.Context, int) ([]platform.ProcessInfo, error) {
	return []platform.ProcessInfo{{PID: 7, StartTicks: 9}}, nil
}
