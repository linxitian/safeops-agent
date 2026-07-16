package main

import (
	"context"
	"flag"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	fileserver "safeops-agent/internal/mcpservers/file"
	"safeops-agent/internal/safefs"
)

func main() {
	root := flag.String("root", "/var/lib/safeops/lab", "absolute SafeOps Lab file root")
	flag.Parse()
	reader, err := safefs.NewReader(*root)
	if err != nil {
		log.Fatal(err)
	}
	if err := fileserver.New(reader).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
