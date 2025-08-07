package exporter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderContainerExporter(t *testing.T) {
	imageExporter := "test-image:latest"
	memoryExporter := "512Mi"
	cpuExporter := "500m"
	resourceExporter := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpuExporter),
		corev1.ResourceMemory: resource.MustParse(memoryExporter),
	}

	clusterValues := &values.SlurmCluster{
		NamespacedName: types.NamespacedName{
			Name:      "test-cluster",
			Namespace: "soperator-ns",
		},
		CRVersion: "1.0.0",
		SlurmExporter: values.SlurmExporter{
			Container: slurmv1.NodeContainer{
				Image:     imageExporter,
				Resources: resourceExporter,
			},
			CollectionInterval: prometheusv1.Duration("30s"),
		},
		NodeRest: values.SlurmREST{
			Service: values.Service{Name: "rest-service"},
			ContainerREST: values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Port: 6817,
				},
			},
		},
	}

	want := corev1.Container{
		Name:  consts.ContainerNameExporter,
		Image: imageExporter,
		Resources: corev1.ResourceRequirements{
			Requests: resourceExporter,
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memoryExporter),
			},
		},
		Args: []string{
			"--cluster-namespace=soperator-ns",
			"--cluster-name=test-cluster",
			"--slurm-api-server=http://rest-service.soperator-ns.svc:6817",
			// NOTE: --collection-interval removed for forward compatibility
		},
		Env: []corev1.EnvVar{
			{Name: "SLURM_EXPORTER_CLUSTER_NAMESPACE", Value: "soperator-ns"},
			{Name: "SLURM_EXPORTER_CLUSTER_NAME", Value: "test-cluster"},
			{Name: "SLURM_EXPORTER_SLURM_API_SERVER", Value: "http://rest-service.soperator-ns.svc:6817"},
			{Name: "SLURM_EXPORTER_COLLECTION_INTERVAL", Value: "30s"},
		},
	}

	got := renderContainerExporter(clusterValues)

	if _, ok := got.Resources.Limits[corev1.ResourceCPU]; ok {
		t.Errorf("ResourceCPU should not be set")
	}
	assert.Equal(t, want.Name, got.Name)
	assert.Equal(t, want.Image, got.Image)
	assert.Equal(t, want.Resources, got.Resources)
	assert.Equal(t, want.Args, got.Args)
	assert.Equal(t, want.Env, got.Env)
}
