package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"safeops-agent/internal/lab"
)

func main() {
	duty := flag.Int("duty", 70, "bounded CPU duty percent, 10-90")
	workers := flag.Int("workers", 1, "bounded worker count")
	duration := flag.Duration("duration", 5*time.Minute, "bounded duration up to 10m")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	config := lab.CPUConfig{DutyPercent: *duty, Workers: *workers, Duration: *duration}
	if err := lab.RunCPU(ctx, config); err != nil {
		log.Fatal(err)
	}
}
