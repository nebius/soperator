package testenv

import (
	"context"
	"os/exec"
	"strings"
)

// IsSlurmClusterCRDsInstalled checks if Slurm Cluster CRDs are installed
func IsSlurmClusterCRDsInstalled(ctx context.Context) bool {
	slurmCRDs := []string{
		"slurmclusters.slurm.nebius.ai",
	}

	cmd := exec.CommandContext(ctx, "kubectl", "get", "crds", "-o", "custom-columns=NAME:.metadata.name")
	output, err := Run(cmd)
	if err != nil {
		return false
	}
	crdList := GetNonEmptyLines(output)
	for _, crd := range slurmCRDs {
		found := false
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// IsFluxCDCRDsInstalled checks if Flux CD CRDs are installed
func IsFluxCDCRDsInstalled(ctx context.Context) bool {
	fluxCRDs := []string{
		"helmreleases.helm.toolkit.fluxcd.io",
		"helmrepositories.source.toolkit.fluxcd.io",
		"kustomizations.kustomize.toolkit.fluxcd.io",
		"gitrepositories.source.toolkit.fluxcd.io",
	}

	cmd := exec.CommandContext(ctx, "kubectl", "get", "crds", "-o", "custom-columns=NAME:.metadata.name")
	output, err := Run(cmd)
	if err != nil {
		return false
	}
	crdList := GetNonEmptyLines(output)
	for _, crd := range fluxCRDs {
		found := false
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// IsKruiseCRDsInstalled checks if OpenKruise CRDs are installed
func IsKruiseCRDsInstalled(ctx context.Context) bool {
	kruiseCRDs := []string{
		"clonesets.apps.kruise.io",
		"statefulsets.apps.kruise.io",
		"daemonsets.apps.kruise.io",
	}

	cmd := exec.CommandContext(ctx, "kubectl", "get", "crds", "-o", "custom-columns=NAME:.metadata.name")
	output, err := Run(cmd)
	if err != nil {
		return false
	}
	crdList := GetNonEmptyLines(output)
	for _, crd := range kruiseCRDs {
		found := false
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// IsCertManagerCRDsInstalled checks if cert-manager CRDs are installed
func IsCertManagerCRDsInstalled(ctx context.Context) bool {
	certManagerCRDs := []string{
		"certificates.cert-manager.io",
		"issuers.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
		"orders.acme.cert-manager.io",
		"challenges.acme.cert-manager.io",
	}

	cmd := exec.CommandContext(ctx, "kubectl", "get", "crds", "-o", "custom-columns=NAME:.metadata.name")
	output, err := Run(cmd)
	if err != nil {
		return false
	}
	crdList := GetNonEmptyLines(output)
	for _, crd := range certManagerCRDs {
		found := false
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
