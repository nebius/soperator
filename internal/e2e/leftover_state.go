package e2e

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// leftoverStateRemovalTargets are substrings of resource addresses that
// pruneLeftoverStateForDestroy drops from a leftover e2e-test state. Each names a
// resource whose stored representation can't be reconciled with the current run's
// config (see pruneLeftoverStateForDestroy).
var leftoverStateRemovalTargets = []string{
	"helm_release",           // SCHED-1618
	"o11y_static_key_secret", // SCHED-1756
}

// pruneLeftoverStateForDestroy drops resources from the current workspace state
// whose stored representation can't be reconciled with the current run's config, so
// they don't wedge the init-time leftover destroy. The shared e2e-test state is
// reused across branches with different module/provider versions, leaving resources
// the current config can't plan a destroy for:
//   - helm_release: the helm provider is configured from a Kubernetes context built
//     out of resources in this same state, which can't be established for a leftover
//     cluster (SCHED-1618).
//   - o11y_static_key_secret: its when=destroy provisioner reads
//     self.triggers_replace.region, absent from the older stored state (SCHED-1756).
//
// The whole cluster is torn down regardless, so these need no terraform-driven
// destroy; removing them lets terraform proceed and restores `terraform show` (which
// healNebiusProviderMismatch relies on). Best-effort: failures are logged and the run
// continues.
//
// Enumeration uses `terraform state list` via our own exec (not the tf handle, whose
// stdout is teed to the log), so it neither dumps the full state nor fails on the
// schema/provisioner drift that breaks `terraform show`.
func pruneLeftoverStateForDestroy(ctx context.Context, tf *tfexec.Terraform) {
	addrs, err := leftoverStateAddresses(ctx, tf)
	if err != nil {
		log.Printf("list terraform state during leftover cleanup failed: %v", err)
		return
	}
	for _, addr := range addressesToPrune(addrs) {
		log.Printf("Removing %s from terraform state", addr)
		if err := tf.StateRm(ctx, addr); err != nil {
			log.Printf("terraform state rm %s failed: %v", addr, err)
		}
	}
}

func leftoverStateAddresses(ctx context.Context, tf *tfexec.Terraform) ([]string, error) {
	cmd := exec.CommandContext(ctx, "terraform", "state", "list")
	cmd.Dir = tf.WorkingDir()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("terraform state list: %w", err)
	}

	var addrs []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			addrs = append(addrs, line)
		}
	}
	return addrs, nil
}

// addressesToPrune returns the state addresses that contain any removal target.
func addressesToPrune(allAddrs []string) []string {
	var pruned []string
	for _, addr := range allAddrs {
		for _, target := range leftoverStateRemovalTargets {
			if strings.Contains(addr, target) {
				pruned = append(pruned, addr)
				break
			}
		}
	}
	return pruned
}
