package worker

import (
	"testing"

	"nebius.ai/slurm-operator/internal/naming"
)

func Test_RenderRoleBinding(t *testing.T) {
	namespace := "test-namespace"
	clusterName := "test-cluster"

	roleBinding := RenderRoleBinding(namespace, clusterName)

	// Check the name
	if roleBinding.Name != naming.BuildRoleBindingWorkerName(clusterName) {
		t.Errorf("Unexpected name: got %v, want %v", roleBinding.Name, naming.BuildRoleBindingWorkerName(clusterName))
	}

	// Check the namespace
	if roleBinding.Namespace != namespace {
		t.Errorf("Unexpected namespace: got %v, want %v", roleBinding.Namespace, namespace)
	}

	// Check the subjects
	if len(roleBinding.Subjects) != 1 || roleBinding.Subjects[0].Kind != "ServiceAccount" || roleBinding.Subjects[0].Name != naming.BuildServiceAccountWorkerName(clusterName) || roleBinding.Subjects[0].Namespace != namespace {
		t.Errorf("Unexpected subjects: got %v, want one subject with kind=ServiceAccount, name=%v, and namespace=%v", roleBinding.Subjects, naming.BuildServiceAccountWorkerName(clusterName), namespace)
	}

	// Check the role reference
	if roleBinding.RoleRef.Kind != "Role" || roleBinding.RoleRef.Name != naming.BuildRoleWorkerName(clusterName) || roleBinding.RoleRef.APIGroup != "rbac.authorization.k8s.io" {
		t.Errorf("Unexpected role reference: got %v, want kind=Role, name=%v, and apiGroup=rbac.authorization.k8s.io", roleBinding.RoleRef, naming.BuildRoleWorkerName(clusterName))
	}
}
