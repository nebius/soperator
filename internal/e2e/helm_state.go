package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// removeHelmReleasesFromState drops every helm_release resource from the current workspace state (SCHED-1618).
// The helm provider is configured from a Kubernetes context derived from resources in this same state
// (the mk8s cluster), so when we clean up a leftover cluster that provider often can't be configured at all
// — and any plan or destroy that touches a helm_release then wedges.
// We tear the cluster down wholesale and don't need a terraform-driven uninstall, so removing the
// releases lets terraform proceed without ever configuring the helm provider.
// Best-effort: any failure is logged and the run continues.
//
// Enumeration uses `terraform state pull` (raw state) rather than `terraform show`,
// which can itself fail to load the helm provider on such a leftover state.
func removeHelmReleasesFromState(ctx context.Context, tf *tfexec.Terraform) {
	addrs, err := helmReleaseAddressesFromState(ctx, tf)
	if err != nil {
		log.Printf("list helm_release resources during state cleanup failed: %v", err)
		return
	}
	for _, addr := range addrs {
		log.Printf("Removing %s from terraform state", addr)
		if err := tf.StateRm(ctx, addr); err != nil {
			log.Printf("terraform state rm %s failed: %v", addr, err)
		}
	}
}

func helmReleaseAddressesFromState(ctx context.Context, tf *tfexec.Terraform) ([]string, error) {
	raw, err := tf.StatePull(ctx)
	if err != nil {
		return nil, fmt.Errorf("pull terraform state: %w", err)
	}
	return helmReleaseAddresses(raw)
}

// helmReleaseAddresses parses raw `terraform state pull` output and
// returns the resource address of every helm_release resource.
// The address omits instance keys, so `terraform state rm` of it removes all instances of the resource.
func helmReleaseAddresses(rawState string) ([]string, error) {
	var st struct {
		Resources []struct {
			Module string `json:"module"`
			Type   string `json:"type"`
			Name   string `json:"name"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(rawState), &st); err != nil {
		return nil, fmt.Errorf("unmarshal terraform state: %w", err)
	}

	var addrs []string
	for _, r := range st.Resources {
		if r.Type != "helm_release" {
			continue
		}
		addr := fmt.Sprintf("%s.%s", r.Type, r.Name)
		if r.Module != "" {
			addr = fmt.Sprintf("%s.%s", r.Module, addr)
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}
