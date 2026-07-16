package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/mcpservers/system"
	"safeops-agent/internal/platform"
)

func main() {
	server := system.New(platform.NewLinux())
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
