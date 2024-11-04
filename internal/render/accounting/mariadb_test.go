package accounting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

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
		NodeAffinity: &corev1.NodeAffinity{},
	}

	antiAffinityEnabled := false

	affinityConfig := getAffinityConfig(affinity, &antiAffinityEnabled)

	assert.Equal(t, affinity.NodeAffinity, affinityConfig.NodeAffinity)
	assert.Equal(t, &antiAffinityEnabled, affinityConfig.AntiAffinityEnabled)
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
