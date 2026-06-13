package worker_test

import (
	"encoding/json"
	"testing"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/resourcepatch"
	"nebius.ai/slurm-operator/internal/values"
)

func patchTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))
	return scheme
}

func jsonValue(t *testing.T, v any) *apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return &apiextensionsv1.JSON{Raw: raw}
}

func renderWorkerStatefulSet(t *testing.T) *kruisev1b1.StatefulSet {
	t.Helper()
	nodeSet := &values.SlurmNodeSet{
		Name: "test-nodeset",
		ParentalCluster: client.ObjectKey{
			Namespace: "test-namespace",
			Name:      "test-cluster",
		},
		ContainerSlurmd: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:           "test-image",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Resources: corev1.ResourceList{
					corev1.ResourceMemory:           resource.MustParse("1Gi"),
					corev1.ResourceCPU:              resource.MustParse("100m"),
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
			},
		},
		ContainerMunge: values.Container{
			NodeContainer: slurmv1.NodeContainer{Image: "munge-image"},
		},
		VolumeSpool:              corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/spool"}},
		VolumeJail:               corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/jail"}},
		StatefulSet:              values.StatefulSet{Replicas: 1},
		ServiceUmbrella:          values.Service{Name: "test-umbrella"},
		SupervisorDConfigMapName: "supervisord-config",
		SSHDConfigMapName:        "sshd-config",
		GPU:                      &slurmv1alpha1.GPUSpec{Enabled: false},
	}

	sts, err := worker.RenderNodeSetStatefulSet(
		"test-cluster",
		nodeSet,
		&slurmv1.Secrets{},
		consts.CGroupV2,
		false,
		false,
		"",
	)
	require.NoError(t, err)
	return &sts
}

func TestPatchWorkerStatefulSet_GpuLimitAndAnnotation(t *testing.T) {
	scheme := patchTestScheme(t)
	sts := renderWorkerStatefulSet(t)
	require.NotEmpty(t, sts.Spec.Template.Spec.Containers)

	policy := slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-patch", Namespace: "test-namespace"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{
				Group: "slurm.nebius.ai", Kind: "NodeSet", Name: "test-nodeset",
			},
			Type: slurmv1alpha1.JSONPatchType,
			Patches: []slurmv1alpha1.ResourcePatch{{
				ResourceRef: slurmv1alpha1.ResourceSelector{
					Kind: "StatefulSet", Name: ptr.To(sts.Name),
				},
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{
					{
						Op:    "add",
						Path:  "/spec/template/metadata/annotations",
						Value: jsonValue(t, map[string]string{"ad.datadoghq.com/worker.checks": "{}"}),
					},
					{
						Op:    "add",
						Path:  "/spec/template/spec/containers/0/resources/limits/nvidia.com~1gpu",
						Value: jsonValue(t, "8"),
					},
				},
			}},
		},
	}

	results, err := resourcepatch.Apply(scheme, sts, []slurmv1alpha1.ResourcePatchPolicy{policy})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Applied, "message: %s", results[0].Message)

	assert.Equal(t, "{}", sts.Spec.Template.Annotations["ad.datadoghq.com/worker.checks"])
	gpu := sts.Spec.Template.Spec.Containers[0].Resources.Limits["nvidia.com/gpu"]
	assert.Equal(t, "8", gpu.String())
}

func TestPatchWorkerStatefulSet_AddNodeSelector(t *testing.T) {
	scheme := patchTestScheme(t)
	sts := renderWorkerStatefulSet(t)

	policy := slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-ns", Namespace: "test-namespace"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{
				Group: "slurm.nebius.ai", Kind: "NodeSet", Name: "test-nodeset",
			},
			Type: slurmv1alpha1.JSONPatchType,
			Patches: []slurmv1alpha1.ResourcePatch{{
				ResourceRef: slurmv1alpha1.ResourceSelector{Kind: "StatefulSet", Name: ptr.To(sts.Name)},
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{
					{Op: "add", Path: "/spec/template/spec/nodeSelector", Value: jsonValue(t, map[string]string{
						"node-pool": "gpu",
					})},
				},
			}},
		},
	}

	results, err := resourcepatch.Apply(scheme, sts, []slurmv1alpha1.ResourcePatchPolicy{policy})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Applied, "message: %s", results[0].Message)
	assert.Equal(t, "gpu", sts.Spec.Template.Spec.NodeSelector["node-pool"])
}

func TestPatchWorkerStatefulSet_RejectsNameChange(t *testing.T) {
	scheme := patchTestScheme(t)
	sts := renderWorkerStatefulSet(t)
	original := sts.Name

	policy := slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-bad", Namespace: "test-namespace"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{
				Group: "slurm.nebius.ai", Kind: "NodeSet", Name: "test-nodeset",
			},
			Type: slurmv1alpha1.JSONPatchType,
			Patches: []slurmv1alpha1.ResourcePatch{{
				ResourceRef: slurmv1alpha1.ResourceSelector{Kind: "StatefulSet", Name: ptr.To(sts.Name)},
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{
					{Op: "replace", Path: "/metadata/name", Value: jsonValue(t, "hijacked")},
				},
			}},
		},
	}

	results, err := resourcepatch.Apply(scheme, sts, []slurmv1alpha1.ResourcePatchPolicy{policy})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Applied)
	assert.Equal(t, original, sts.Name)
}
