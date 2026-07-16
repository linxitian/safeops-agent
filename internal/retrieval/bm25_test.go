package retrieval

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLoadAndBM25RanksPortConflict(t *testing.T) {
	documents, err := LoadDirectory(filepath.Join("..", "..", "knowledge"))
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 3 {
		t.Fatalf("got %d docs", len(documents))
	}
	retriever, err := NewBM25(documents)
	if err != nil {
		t.Fatal(err)
	}
	results, err := retriever.Search(context.Background(), "Web 服务 EADDRINUSE 端口被进程占用", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].DocumentID != "case-port-conflict-eaddrinuse" || len(results[0].MatchedTerms) == 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
}
