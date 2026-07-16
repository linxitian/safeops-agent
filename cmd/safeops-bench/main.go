package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"safeops-agent/internal/benchmark"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	policyPath := flags.String("policy", "./policies/tools.yaml", "local tool security policy")
	outputDir := flags.String("output-dir", "./artifacts/benchmark", "benchmark report output directory")
	_ = flags.Parse(os.Args[2:])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := benchmark.Run(ctx, command, *policyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		usage()
		os.Exit(2)
	}
	jsonPath, markdownPath, err := benchmark.Write(report, *outputDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s and %s\n", jsonPath, markdownPath)
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if report.Failed() {
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: safeops-bench <%s> [--policy path] [--output-dir path]\n", strings.Join(benchmark.Commands, "|"))
}
