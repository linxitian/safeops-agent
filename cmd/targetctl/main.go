package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"safeops-agent/internal/target"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	mcpConfig := flags.String("mcp-config", "./config/mcp_servers.yaml", "MCP manifest path")
	outputDir := flags.String("output-dir", "./artifacts/target", "report output directory")
	_ = flags.Parse(os.Args[2:])
	var report target.Report
	switch command {
	case "probe":
		report = target.Probe(ctx)
	case "test":
		report = target.Test(ctx, *mcpConfig)
	case "doctor":
		report = target.Doctor(ctx, *mcpConfig)
	case "report":
		report = target.Doctor(ctx, *mcpConfig)
		report.Scope = "report"
	default:
		usage()
		os.Exit(2)
	}
	jsonPath, textPath, err := target.Write(report, *outputDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s and %s\n", jsonPath, textPath)
	_ = json.NewEncoder(os.Stdout).Encode(report)
	if report.Overall == target.Fail {
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: targetctl <probe|test|doctor|report> [--mcp-config path] [--output-dir path]")
}
