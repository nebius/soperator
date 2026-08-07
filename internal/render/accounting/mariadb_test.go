package accounting

import (
	"testing"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/v25/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_GetMariaDbConfig(t *testing.T) {
	mariaDb := slurmv1.MariaDbOperator{
		Enabled: true,
		NodeContainer: slurmv1.NodeContainer{
			Image: "mariadb:10.5",
			Port:  3306,
		},
		Replicas: 2,
	}

	port, replicas, antiAffinityEnabled := getMariaDbConfig(mariaDb)

	assert.Equal(t, int32(3306), port)
	assert.Equal(t, int32(2), replicas)
	assert.Equal(t, true, *antiAffinityEnabled)
}

func Test_GetAffinityConfig(t *testing.T) {
	affinity := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/e2e-az-name",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"e2e-az1", "e2e-az2"},
							},
						},
					},
				},
			},
		},
	}

	antiAffinityEnabled := false

	affinityConfig := getAffinityConfig(affinity, &antiAffinityEnabled)

	assert.Equal(t, &antiAffinityEnabled, affinityConfig.AntiAffinityEnabled)
	assert.Equal(
		t,
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key,
		affinityConfig.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key,
	)
	assert.Equal(
		t,
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Operator,
		affinityConfig.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Operator,
	)
	assert.Equal(
		t,
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values,
		affinityConfig.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values,
	)
}

func Test_RenderMariaDb(t *testing.T) {
	namespace := "test-namespace"
	clusterName := "test-cluster"
	imageMariaDb := "mariadb:10.5"
	replicas := int32(2)
	portMariadb := int32(3306)
	nodeFilterName := "cpu"
	accounting := &values.SlurmAccounting{
		MariaDb: slurmv1.MariaDbOperator{

			Replicas: replicas,
			Enabled:  true,
			NodeContainer: slurmv1.NodeContainer{
				Image: imageMariaDb,
				Port:  portMariadb,
			},
		},
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: nodeFilterName,
		},
	}
	nodeFilters := []slurmv1.K8sNodeFilter{
		{
			Name: nodeFilterName,
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{},
			},
		},
	}

	mariaDb, err := RenderMariaDb(namespace, clusterName, accounting, nodeFilters)

	assert.NoError(t, err)
	assert.NotNil(t, mariaDb)
	assert.Equal(t, namespace, mariaDb.Namespace)
	assert.Equal(t, clusterName+"-"+consts.MariaDbClusterSuffix, mariaDb.Name)
	assert.Equal(t, imageMariaDb, mariaDb.Spec.Image)
	assert.Equal(t, replicas, mariaDb.Spec.Replicas)
	assert.Equal(t, portMariadb, mariaDb.Spec.Port)
	assert.Equal(t, consts.MariaDbDatabase, *mariaDb.Spec.Database)
	assert.Equal(t, consts.MariaDbUsername, *mariaDb.Spec.Username)
	assert.Equal(t, consts.MariaDbSecretName, mariaDb.Spec.PasswordSecretKeyRef.SecretKeySelector.Name)
	assert.Equal(t, consts.MariaDbPasswordKey, mariaDb.Spec.PasswordSecretKeyRef.SecretKeySelector.Key)
	assert.Equal(t, true, mariaDb.Spec.PasswordSecretKeyRef.Generate)
	assert.Equal(t, consts.MariaDbSecretRootName, mariaDb.Spec.RootPasswordSecretKeyRef.SecretKeySelector.Name)
	assert.Equal(t, consts.MariaDbPasswordKey, mariaDb.Spec.RootPasswordSecretKeyRef.SecretKeySelector.Key)
}

func Test_RenderMariaDb_CustomLabelsAndAnnotations(t *testing.T) {
	acc := &values.SlurmAccounting{
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "test-filter",
		},
		Labels:      map[string]string{"gcore.com/project-id": "123"},
		Annotations: map[string]string{"gcore.com/note": "abc"},
		MariaDb: slurmv1.MariaDbOperator{
			Enabled: true,
			NodeContainer: slurmv1.NodeContainer{
				Image: "mariadb:10.5",
			},
			Storage: mariadbv1alpha1.Storage{
				Size: ptr.To(resource.MustParse("1Gi")),
			},
		},
	}

	nodeFilters := []slurmv1.K8sNodeFilter{{Name: "test-filter"}}

	result, err := RenderMariaDb("test-namespace", "test-cluster", acc, nodeFilters)
	assert.NoError(t, err)

	assert.NotNil(t, result.Spec.PodTemplate.PodMetadata)
	assert.Equal(t, "123", result.Spec.PodTemplate.PodMetadata.Labels["gcore.com/project-id"])
	assert.Equal(t, "abc", result.Spec.PodTemplate.PodMetadata.Annotations["gcore.com/note"])
}
