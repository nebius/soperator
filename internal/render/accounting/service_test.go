package accounting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"
	"nebius.ai/slurm-operator/internal/consts"
	accounting "nebius.ai/slurm-operator/internal/render/accounting"
	"nebius.ai/slurm-operator/internal/render/common"
)

func Test_RenderService(t *testing.T) {
	namespace := "test-namespace"
	clusterName := "test-cluster"

	service, err := accounting.RenderService(namespace, clusterName, *acc)
	assert.NoError(t, err)

	assert.Equal(t, acc.Service.Name, service.Name)
	assert.Equal(t, namespace, service.Namespace)
	assert.Equal(t, common.RenderLabels(consts.ComponentTypeAccounting, clusterName), service.Labels)

	assert.Equal(t, acc.Service.Type, service.Spec.Type)
	assert.Equal(t, common.RenderMatchLabels(consts.ComponentTypeAccounting, clusterName), service.Spec.Selector)
	assert.Equal(t, "", service.Spec.ClusterIP)
	assert.Equal(t, acc.Service.Protocol, service.Spec.Ports[0].Protocol)
	assert.Equal(t, acc.ContainerAccounting.Port, service.Spec.Ports[0].Port)
	assert.Equal(t, intstr.FromString(acc.ContainerAccounting.Name), service.Spec.Ports[0].TargetPort)
}
