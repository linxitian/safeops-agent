package diagnostic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/platform"
	"safeops-agent/internal/retrieval"
)

func TestDiagnosticToolsOverMCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, ct := mcp.NewInMemoryTransports()
	retriever, err := retrieval.NewBM25([]retrieval.KnowledgeDocument{{DocumentID: "port-case", Title: "EADDRINUSE 端口冲突", Content: "address already in use", Source: "test-fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	go New(fakeProvider{}, retriever).Run(ctx, st)
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
		if tool.OutputSchema == nil {
			t.Fatalf("missing output schema for %s", tool.Name)
		}
	}
	if count != 4 {
		t.Fatalf("got %d tools", count)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "diagnostic.port_conflict", Arguments: map[string]any{"unit": "demo.service", "port": 8080}})
	if err != nil || result.IsError {
		t.Fatalf("unexpected result %v %+v", err, result)
	}
	b, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"document_id":"port-case"`) || !strings.Contains(string(b), `"case_similarity":`) {
		t.Fatalf("missing retrieval provenance or confidence component: %s", b)
	}
}

type fakeProvider struct{}

func (fakeProvider) Service(context.Context, string) (platform.ServiceStatus, error) {
	return platform.ServiceStatus{Name: "demo.service", ActiveState: "failed"}, nil
}
func (fakeProvider) Journal(context.Context, platform.JournalQuery) ([]platform.JournalEvent, error) {
	return []platform.JournalEvent{{Message: "EADDRINUSE"}}, nil
}
func (fakeProvider) Sockets(context.Context, bool, int) ([]platform.SocketInfo, error) {
	return []platform.SocketInfo{{LocalPort: 8080, Listening: true}}, nil
}
func (fakeProvider) ProcessesByPort(context.Context, int) ([]platform.ProcessInfo, error) {
	return []platform.ProcessInfo{{PID: 7, StartTicks: 9}}, nil
}
func (fakeProvider) Processes(context.Context, platform.ProcessQuery) ([]platform.ProcessInfo, error) {
	return []platform.ProcessInfo{{PID: 7, StartTicks: 9}}, nil
}
func (fakeProvider) Load(context.Context) (platform.LoadAverage, error) {
	return platform.LoadAverage{One: 1}, nil
}
func (fakeProvider) Memory(context.Context) (platform.MemoryStat, error) {
	return platform.MemoryStat{TotalBytes: 100}, nil
}
func (fakeProvider) Disk(context.Context, string) (platform.DiskUsage, error) {
	return platform.DiskUsage{Path: "/", TotalBytes: 100, UsedBytes: 50, UsedRatio: .5}, nil
}
func (fakeProvider) Kernel(context.Context) (platform.KernelInfo, error) {
	return platform.KernelInfo{OS: "linux"}, nil
}
