package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"safeops-agent/internal/lab"
)

func main() {
	root := flag.String("root", "/var/lib/safeops/lab", "absolute Lab root")
	path := flag.String("path", "/var/lib/safeops/lab/growth.log", "absolute bounded log path")
	maxBytes := flag.Int64("max-bytes", 32<<20, "total file size cap, maximum 64 MiB")
	rate := flag.Int64("rate", 256<<10, "write rate cap, maximum 1 MiB/s")
	duration := flag.Duration("duration", 5*time.Minute, "bounded duration up to 10m")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	result, err := lab.WriteBoundedLog(ctx, lab.LogConfig{Root: *root, Path: *path, MaxBytes: *maxBytes, RateBytes: *rate, Duration: *duration})
	if err != nil {
		log.Fatal(err)
	}
	_ = json.NewEncoder(log.Writer()).Encode(result)
}
