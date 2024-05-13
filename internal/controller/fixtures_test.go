package controller_test

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func minimalSlurmClusterFixture(namespace string) *slurmv1.SlurmCluster {
	return &slurmv1.SlurmCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "slurm.nebius.ai/v1",
			Kind:       SlurmClusterKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minimal-slurm-cluster",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "slurmcluster",
				"app.kubernetes.io/instance":   "test-slurm-cluster",
				"app.kubernetes.io/part-of":    "slurm-operator",
				"app.kubernetes.io/managed-by": "kustomize",
				"app.kubernetes.io/created-by": "slurm-operator",
			},
		},
		Spec: slurmv1.SlurmClusterSpec{},
	}
}
