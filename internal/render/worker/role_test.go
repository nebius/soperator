package worker

import (
	"testing"

	"nebius.ai/slurm-operator/internal/naming"
)

func Test_RenderRole(t *testing.T) {
	namespace := "test-namespace"
	clusterName := "test-cluster"

	role := RenderRole(namespace, clusterName)

	// Check the name
	if role.Name != naming.BuildRoleWorkerName(clusterName) {
		t.Errorf("Unexpected name: got %v, want %v", role.Name, naming.BuildRoleWorkerName(clusterName))
	}

	// Check the namespace
	if role.Namespace != namespace {
		t.Errorf("Unexpected namespace: got %v, want %v", role.Namespace, namespace)
	}

	// Check the rules
	if len(role.Rules) != 1 || role.Rules[0].APIGroups[0] != "" || role.Rules[0].Resources[0] != "events" || role.Rules[0].Verbs[0] != "create" {
		t.Errorf("Unexpected rules: got %v, want one rule with apiGroups=[\"\"], resources=[\"events\"], and verbs=[\"create\"]", role.Rules)
	}
}
