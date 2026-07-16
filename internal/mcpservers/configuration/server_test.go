package configuration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/safefs"
)

func TestConfigSnapshotAndDiff(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.yaml")
	if err := os.WriteFile(path, []byte("value: one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, _ := safefs.NewReader(root)
	baseline, err := reader.Snapshot(context.Background(), root, 10, 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("value: two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if count != 4 {
		t.Fatalf("got %d tools", count)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "config.diff_snapshot", Arguments: map[string]any{"path": root, "baseline": baseline.Entries}})
	if err != nil || result.IsError {
		t.Fatalf("unexpected result: %+v %v", result, err)
	}
	structured := result.StructuredContent.(map[string]any)
	if len(structured["modified"].([]any)) != 1 {
		t.Fatalf("expected one modified file: %#v", structured)
	}
}

func TestConfigRejectsForgedBaselineHash(t *testing.T) {
	if err := validateBaseline([]safefs.SnapshotEntry{{RelativePath: "app.yaml", SHA256: "not-a-hash"}}); err == nil {
		t.Fatal("invalid baseline hash accepted")
	}
}
