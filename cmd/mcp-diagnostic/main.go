package main

import (
	"context"
	"flag"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	diagnosticserver "safeops-agent/internal/mcpservers/diagnostic"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/retrieval"
)

func main() {
	knowledgeDir := flag.String("knowledge", "./knowledge", "versioned local knowledge directory")
	flag.Parse()
	documents, err := retrieval.LoadDirectory(*knowledgeDir)
	if err != nil {
		log.Fatal(err)
	}
	retriever, err := retrieval.NewBM25(documents)
	if err != nil {
		log.Fatal(err)
	}
	provider := diagnosticserver.CombinedProvider{Linux: platform.NewLinux(), Commands: platform.NewCommandPlatform()}
	if err := diagnosticserver.New(provider, retriever).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
