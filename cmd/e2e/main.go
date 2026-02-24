package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/kelseyhightower/envconfig"

	"nebius.ai/slurm-operator/internal/e2e"
)

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: e2e <apply|destroy|check-capacity>\n")
		os.Exit(2)
	}

	profile, err := e2e.LoadProfile()
	if err != nil {
		log.Fatalf("Load profile: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "check-capacity":
		err = runCheckCapacity(ctx, profile)
	case "apply":
		cfg := loadFullConfig(profile)
		err = e2e.Apply(ctx, cfg)
	case "destroy":
		cfg := loadFullConfig(profile)
		err = e2e.Destroy(ctx, cfg)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: e2e <apply|destroy|check-capacity>\n", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("%s: %v", os.Args[1], err)
	}
}

func loadFullConfig(profile e2e.Profile) e2e.Config {
	var cfg e2e.Config
	if err := envconfig.Process("", &cfg); err != nil {
		log.Fatalf("Parse config: %v", err)
	}
	cfg.Profile = profile

	sshPubKey, err := e2e.GenerateSSHPublicKey()
	if err != nil {
		log.Fatalf("Generate SSH public key: %v", err)
	}
	cfg.SSHPublicKey = sshPubKey

	return cfg
}

func runCheckCapacity(ctx context.Context, profile e2e.Profile) error {
	err := e2e.CheckCapacity(ctx, profile)
	if !errors.Is(err, e2e.ErrInsufficientCapacity) {
		return err
	}

	log.Print("Insufficient capacity detected with cancel strategy, cancelling workflow")
	runID := os.Getenv("GITHUB_RUN_ID")
	if runID == "" {
		return fmt.Errorf("GITHUB_RUN_ID is not set, cannot cancel workflow")
	}

	cmd := exec.CommandContext(ctx, "gh", "run", "cancel", runID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if cancelErr := cmd.Run(); cancelErr != nil {
		return fmt.Errorf("cancel workflow run %s: %w", runID, cancelErr)
	}

	log.Printf("Workflow run %s cancelled due to insufficient capacity", runID)
	return nil
}
