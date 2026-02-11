package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"nebius.ai/slurm-operator/internal/e2e/tfrunner"
)

func Init(cfg Config) (runner *tfrunner.Runner, cleanup func(), err error) {
	runner, cleanup, err = setupRunner(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("setup terraform runner: %w", err)
	}

	if err := runner.Init(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("terraform init: %w", err)
	}
	if err := runner.WorkspaceSelectOrNew("e2e-test"); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("select workspace: %w", err)
	}

	logState(runner)
	return runner, cleanup, nil
}

func setupRunner(cfg Config) (runner *tfrunner.Runner, cleanup func(), err error) {
	tfVars, err := readTFVars(fmt.Sprintf("%s/terraform.tfvars", cfg.PathToInstallation))
	if err != nil {
		return nil, nil, fmt.Errorf("read terraform variables: %w", err)
	}

	tfVars, err = overrideTestValues(tfVars, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("override test values: %w", err)
	}

	varsJSON, err := json.MarshalIndent(tfVars, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal terraform variables to JSON: %w", err)
	}
	log.Printf("Terraform variables:\n%s", varsJSON)

	varFilePath := fmt.Sprintf("%s/e2e-override.tfvars.json", cfg.PathToInstallation)
	if err := os.WriteFile(varFilePath, varsJSON, 0o644); err != nil {
		return nil, nil, fmt.Errorf("write terraform var file %s: %w", varFilePath, err)
	}
	cleanup = func() { _ = os.Remove(varFilePath) }

	envVars := make(map[string]string)
	for _, envVar := range os.Environ() {
		k, v, ok := strings.Cut(envVar, "=")
		if ok {
			envVars[k] = v
		}
	}

	runner = tfrunner.New(tfrunner.Options{
		Dir:      cfg.PathToInstallation,
		VarFiles: []string{varFilePath},
		EnvVars:  envVars,
	})

	return runner, cleanup, nil
}

func logState(runner *tfrunner.Runner) {
	out, err := runner.Run("state", "list")
	if err != nil {
		log.Printf("terraform state list failed: %v", err)
		return
	}
	if out == "" {
		log.Print("Terraform state is empty")
		return
	}
	log.Printf("Terraform state resources:\n%s", out)
}
