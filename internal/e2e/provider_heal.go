package e2e

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

// healNebiusProviderMismatch repairs the cross-provider state wedge.
// The e2e scheduler round-robins main and the release branches through the single shared "e2e-test" workspace,
// but those branches declare the nebius provider under different source FQNs (public registry.terraform.io vs
// the private storage mirror). When the FQN in the leftover state differs from the one the current config wants,
// terraform attempts an automatic cross-provider migration of nebius_mk8s_v1_node_group that the provider does
// not implement, deterministically wedging every init/apply/destroy until the state is repaired.
//
// terraform state replace-provider rewrites the provider references in state directly
// (no provider RPC, version-agnostic, bidirectional, and it backs up the state file first),
// which is the only thing that clears the wedge. This is best-effort: any failure is logged and the run continues,
// so the heal can never make a run worse than the unhealed wedge it is trying to fix.
//
// The current workspace must already be selected before calling this.
func healNebiusProviderMismatch(ctx context.Context, tf *tfexec.Terraform, installDir string) {
	stateFQN := nebiusProviderInState(ctx, tf)
	if stateFQN == "" {
		return
	}
	configFQN := nebiusProviderInConfig(ctx, tf, stateFQN)
	if configFQN == "" || configFQN == stateFQN {
		return
	}

	log.Printf("Healing nebius provider FQN in e2e-test state: %s -> %s (SCHED-1983)", stateFQN, configFQN)
	cmd := exec.CommandContext(ctx, "terraform", "state", "replace-provider", "-auto-approve", stateFQN, configFQN)
	cmd.Dir = installDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("terraform state replace-provider %s -> %s failed: %v", stateFQN, configFQN, err)
	}
}

// nebiusProviderInState returns the nebius provider FQN under which resources are
// currently recorded in state, or "" if state is empty or has no nebius resource.
func nebiusProviderInState(ctx context.Context, tf *tfexec.Terraform) string {
	state, err := tf.Show(ctx)
	if err != nil {
		log.Printf("terraform show failed during provider heal: %v", err)
		return ""
	}
	return firstNebiusProviderInState(state)
}

// nebiusProviderInConfig returns the nebius provider FQN the current config resolves to.
// When the config schema lists more than one nebius FQN, it prefers the one that differs from stateFQN
// (the migration target).
func nebiusProviderInConfig(ctx context.Context, tf *tfexec.Terraform, stateFQN string) string {
	schemas, err := tf.ProvidersSchema(ctx)
	if err != nil {
		log.Printf("terraform providers schema failed during provider heal: %v", err)
		return ""
	}
	var nebiusFQNs []string
	for fqn := range schemas.Schemas {
		if isNebiusProviderFQN(fqn) {
			nebiusFQNs = append(nebiusFQNs, fqn)
		}
	}
	return pickTargetNebiusFQN(nebiusFQNs, stateFQN)
}

// pickTargetNebiusFQN selects the migration target from the nebius FQNs the
// config declares: the first that differs from stateFQN, else the only one
// present (which signals an already-aligned state), else "".
func pickTargetNebiusFQN(configNebiusFQNs []string, stateFQN string) string {
	for _, fqn := range configNebiusFQNs {
		if fqn != stateFQN {
			return fqn
		}
	}
	if len(configNebiusFQNs) > 0 {
		return configNebiusFQNs[0]
	}
	return ""
}

func firstNebiusProviderInState(state *tfjson.State) string {
	if state == nil || state.Values == nil {
		return ""
	}
	for _, name := range collectModuleProviderNames(state.Values.RootModule) {
		if isNebiusProviderFQN(name) {
			return name
		}
	}
	return ""
}

func collectModuleProviderNames(mod *tfjson.StateModule) []string {
	if mod == nil {
		return nil
	}
	var names []string
	for _, r := range mod.Resources {
		names = append(names, r.ProviderName)
	}
	for _, child := range mod.ChildModules {
		names = append(names, collectModuleProviderNames(child)...)
	}
	return names
}

// isNebiusProviderFQN matches both the public registry source (registry.terraform.io/nebius/nebius)
// and the private storage mirror (terraform-provider.storage.eu-north1.nebius.cloud/nebius/nebius), as well as
// any future host, by the shared "/nebius/nebius" namespace suffix.
func isNebiusProviderFQN(fqn string) bool {
	return strings.HasSuffix(fqn, "/nebius/nebius")
}
