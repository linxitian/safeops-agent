package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	configurationserver "safeops-agent/internal/mcpservers/configuration"
	"safeops-agent/internal/safefs"
)

func main() {
	roots := flag.String("roots", "/etc/safeops,/var/lib/safeops/lab/config", "comma-separated absolute managed configuration roots")
	flag.Parse()
	reader, err := safefs.NewReader(strings.Split(*roots, ",")...)
	if err != nil {
		log.Fatal(err)
	}
	if err := configurationserver.New(reader).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
