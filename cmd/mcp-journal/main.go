package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log"
	journalserver "safeops-agent/internal/mcpservers/journal"
	"safeops-agent/internal/platform"
)

func main() {
	if err := journalserver.New(platform.NewCommandPlatform()).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
