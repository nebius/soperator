package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

func Init(ctx context.Context, cfg Config) (tf *tfexec.Terraform, varFilePath string, cleanup func(), err error) {
	tf, varFilePath, cleanup, err = setupTerraform(cfg)
	if err != nil {
		return nil, "", nil, fmt.Errorf("setup terraform: %w", err)
	}

	if err := tf.Init(ctx); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("terraform init: %w", err)
	}

	if err := workspaceSelectOrNew(ctx, tf, "e2e-test"); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("select workspace: %w", err)
	}

	logState(ctx, tf)
	return tf, varFilePath, cleanup, nil
}

func setupTerraform(cfg Config) (tf *tfexec.Terraform, varFilePath string, cleanup func(), err error) {
	tfVars, err := readTFVars(fmt.Sprintf("%s/terraform.tfvars", cfg.PathToInstallation))
	if err != nil {
		return nil, "", nil, fmt.Errorf("read terraform variables: %w", err)
	}

	tfVars, err = overrideTestValues(tfVars, cfg)
	if err != nil {
		return nil, "", nil, fmt.Errorf("override test values: %w", err)
	}

	varsJSON, err := json.MarshalIndent(tfVars, "", "  ")
	if err != nil {
		return nil, "", nil, fmt.Errorf("marshal terraform variables to JSON: %w", err)
	}
	log.Printf("Terraform variables:\n%s", varsJSON)

	varFilePath = fmt.Sprintf("%s/e2e-override.tfvars.json", cfg.PathToInstallation)
	if err := os.WriteFile(varFilePath, varsJSON, 0o644); err != nil {
		return nil, "", nil, fmt.Errorf("write terraform var file %s: %w", varFilePath, err)
	}
	cleanup = func() { _ = os.Remove(varFilePath) }

	execPath, err := exec.LookPath("terraform")
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("find terraform binary: %w", err)
	}

	tf, err = tfexec.NewTerraform(cfg.PathToInstallation, execPath)
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("create terraform executor: %w", err)
	}
	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)

	return tf, varFilePath, cleanup, nil
}

func workspaceSelectOrNew(ctx context.Context, tf *tfexec.Terraform, name string) error {
	workspaces, _, err := tf.WorkspaceList(ctx)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	if slices.Contains(workspaces, name) {
		return tf.WorkspaceSelect(ctx, name)
	}
	return tf.WorkspaceNew(ctx, name)
}

func logState(ctx context.Context, tf *tfexec.Terraform) {
	state, err := tf.Show(ctx)
	if err != nil {
		log.Printf("terraform show failed: %v", err)
		return
	}
	addrs := collectResourceAddresses(state)
	if len(addrs) == 0 {
		log.Print("Terraform state is empty")
		return
	}
	log.Printf("Terraform state resources:\n%s", strings.Join(addrs, "\n"))
}

func collectResourceAddresses(state *tfjson.State) []string {
	if state.Values == nil {
		return nil
	}
	return collectModuleResources(state.Values.RootModule)
}

func collectModuleResources(mod *tfjson.StateModule) []string {
	if mod == nil {
		return nil
	}
	var addrs []string
	for _, r := range mod.Resources {
		addrs = append(addrs, r.Address)
	}
	for _, child := range mod.ChildModules {
		addrs = append(addrs, collectModuleResources(child)...)
	}
	return addrs
}
