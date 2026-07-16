package journal

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"safeops-agent/internal/platform"
	"testing"
	"time"
)

func TestJournalToolsOverMCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, ct := mcp.NewInMemoryTransports()
	go New(fakeJournal{}).Run(ctx, st)
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
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "journal.search_errors", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("unexpected result %v %+v", err, result)
	}
}

type fakeJournal struct{}

func (fakeJournal) Journal(context.Context, platform.JournalQuery) ([]platform.JournalEvent, error) {
	return []platform.JournalEvent{{Priority: 3, Message: "failed to start"}}, nil
}
