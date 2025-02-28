package accounting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/accounting"
	"nebius.ai/slurm-operator/internal/render/common"
)

func Test_RenderDeployment(t *testing.T) {

	deployment, err := accounting.RenderDeployment(defaultNamespace, defaultNameCluster, acc, defaultNodeFilter, defaultVolumeSources, slurmTopologyConfigMapRefName)
	assert.NoError(t, err)

	assert.Equal(t, naming.BuildDeploymentName(consts.ComponentTypeAccounting), deployment.Name)
	assert.Equal(t, defaultNamespace, deployment.Namespace)
	assert.Equal(t, common.RenderLabels(consts.ComponentTypeAccounting, defaultNameCluster), deployment.Labels)

	assert.Equal(t, &acc.Deployment.Replicas, deployment.Spec.Replicas)
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)
	assert.Equal(t, common.RenderMatchLabels(consts.ComponentTypeAccounting, defaultNameCluster), deployment.Spec.Selector.MatchLabels)

}
