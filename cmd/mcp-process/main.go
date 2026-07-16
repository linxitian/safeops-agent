package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log"
	processserver "safeops-agent/internal/mcpservers/process"
	"safeops-agent/internal/platform"
)

func main() {
	if err := processserver.New(platform.NewLinux()).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
