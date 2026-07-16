package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log"
	serviceserver "safeops-agent/internal/mcpservers/service"
	"safeops-agent/internal/platform"
)

func main() {
	if err := serviceserver.New(platform.NewCommandPlatform()).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
