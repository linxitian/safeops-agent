package service

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"safeops-agent/internal/platform"
	"testing"
	"time"
)

func TestServiceToolsOverMCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, ct := mcp.NewInMemoryTransports()
	go New(fakeService{}).Run(ctx, st)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
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
			t.Fatalf("missing schema for %s", tool.Name)
		}
	}
	if count != 4 {
		t.Fatalf("got %d tools", count)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "service.get_status", Arguments: map[string]any{"unit": "demo"}})
	if err != nil || result.IsError {
		t.Fatalf("unexpected result %v %+v", err, result)
	}
}

type fakeService struct{}

func (fakeService) Service(context.Context, string) (platform.ServiceStatus, error) {
	return platform.ServiceStatus{Name: "demo.service", ActiveState: "active", MainPID: 5}, nil
}
func (fakeService) FailedServices(context.Context) ([]platform.ServiceStatus, error) {
	return []platform.ServiceStatus{}, nil
}
func (fakeService) ServiceDependencies(context.Context, string) (platform.ServiceDependencies, error) {
	return platform.ServiceDependencies{Name: "demo.service"}, nil
}
