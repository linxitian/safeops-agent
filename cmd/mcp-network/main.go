package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log"
	networkserver "safeops-agent/internal/mcpservers/network"
	"safeops-agent/internal/platform"
)

func main() {
	if err := networkserver.New(platform.NewLinux()).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
