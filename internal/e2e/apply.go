package e2e

import (
	"fmt"
	"log"
)

func Apply(cfg Config) error {
	runner, cleanup, err := Init(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := runner.Destroy(); err != nil {
		log.Printf("Pre-cleanup destroy failed: %v", err)
		logState(runner)
		return fmt.Errorf("pre-cleanup destroy failed, state may contain stuck resources: %w", err)
	}

	if err := runner.Apply(); err != nil {
		return fmt.Errorf("terraform apply: %w", err)
	}
	return nil
}
