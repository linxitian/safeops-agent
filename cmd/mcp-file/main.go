package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	fileserver "safeops-agent/internal/mcpservers/file"
	"safeops-agent/internal/safefs"
)

func main() {
	root := flag.String("root", "", "single absolute file root; kept for compatibility")
	roots := flag.String("roots", "/var/log,/var/lib/safeops/lab", "comma-separated absolute read-only file roots")
	flag.Parse()
	values := strings.Split(*roots, ",")
	if strings.TrimSpace(*root) != "" {
		values = []string{*root}
	}
	reader, err := safefs.NewReader(values...)
	if err != nil {
		log.Fatal(err)
	}
	if err := fileserver.New(reader).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
