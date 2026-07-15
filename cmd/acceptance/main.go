package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"nebius.ai/slurm-operator/internal/e2e/acceptance"
)

func main() {
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := acceptance.Run(ctx, os.Args[1:]); err != nil {
		log.Fatalf("acceptance: %v", err)
	}
}
