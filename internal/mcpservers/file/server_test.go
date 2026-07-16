package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/safefs"
)

func TestFileToolsUseAllowlistedReader(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "demo.log")
	if err := os.WriteFile(path, []byte("safeops"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, _ := safefs.NewReader(root)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go New(reader).Run(ctx, serverTransport)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	count := 0
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil || tool.OutputSchema == nil {
			t.Fatalf("invalid tool: %+v %v", tool, err)
		}
		count++
	}
	if count != 5 {
		t.Fatalf("got %d tools", count)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "file.sha256", Arguments: map[string]any{"path": path}})
	if err != nil || result.IsError || result.StructuredContent == nil {
		t.Fatalf("unexpected result: %+v %v", result, err)
	}
}
