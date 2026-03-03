package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kelseyhightower/envconfig"

	"nebius.ai/slurm-operator/internal/e2e"
)

func main() {
	if len(os.Args) < 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: e2e <apply|destroy>\n")
		os.Exit(2)
	}

	var cfg e2e.Config
	if err := envconfig.Process("", &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "apply":
		err = e2e.Apply(ctx, cfg)
	case "destroy":
		err = e2e.Destroy(ctx, cfg)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: e2e <apply|destroy>\n", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("%s: %v", os.Args[1], err)
	}
}
