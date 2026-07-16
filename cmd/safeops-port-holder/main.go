package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os/signal"
	"syscall"

	"safeops-agent/internal/lab"
)

func main() {
	address := flag.String("listen", "127.0.0.1:18081", "loopback Lab listen address")
	flag.Parse()
	if err := lab.ValidateLoopbackAddress(*address); err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Printf("SafeOps Lab port holder listening on %s", listener.Addr())
	<-ctx.Done()
}
