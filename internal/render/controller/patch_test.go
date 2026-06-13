package controller

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
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
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

// renderControllerStatefulSet renders a representative controller StatefulSet
// (an OpenKruise Advanced StatefulSet).
func renderControllerStatefulSet(t *testing.T) *kruisev1b1.StatefulSet {
	t.Helper()
	controller := &values.SlurmController{
		K8sNodeFilterName: "test-filter",
		StatefulSet: values.StatefulSet{
			Name:           "test-cluster-controller",
			Replicas:       1,
			MaxUnavailable: intstr.FromInt32(1),
		},
		Service: values.Service{Name: "test-controller-svc"},
		ContainerSlurmctld: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:           "test-image:latest",
				ImagePullPolicy: corev1.PullAlways,
				Port:            6817,
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				AppArmorProfile: consts.AppArmorProfileUnconfined,
			},
			Name: "slurmctld",
		},
		ContainerMunge: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:           "munge-image:latest",
				ImagePullPolicy: corev1.PullAlways,
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
				AppArmorProfile: consts.AppArmorProfileUnconfined,
			},
		},
		VolumeSpool:   slurmv1.NodeVolume{VolumeSourceName: ptr.To("test-volume")},
		VolumeJail:    slurmv1.NodeVolume{VolumeSourceName: ptr.To("test-volume")},
		PriorityClass: "test-priority",
	}

	nodeFilters := []slurmv1.K8sNodeFilter{{
		Name: "test-filter",
		Affinity: &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      "node-type",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"controller"},
						}},
					}},
				},
			},
		},
	}}
	volumeSources := []slurmv1.VolumeSource{{
		Name:         "test-volume",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}

	sts, err := RenderStatefulSet("test-namespace", "test-cluster", nodeFilters, volumeSources, controller, true)
	require.NoError(t, err)
	return &sts
}

func TestPatchControllerStatefulSet_MeshAnnotationAndResources(t *testing.T) {
	scheme := patchTestScheme(t)
	sts := renderControllerStatefulSet(t)
	require.NotEmpty(t, sts.Spec.Template.Spec.Containers)

	policy := slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "ctl-patch", Namespace: "test-namespace"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{
				Group: "slurm.nebius.ai", Kind: "SlurmCluster", Name: "test-cluster",
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
						Value: jsonValue(t, map[string]string{"sidecar.istio.io/inject": "true"}),
					},
					{
						Op:   "add",
						Path: "/spec/template/spec/containers/0/resources/limits",
						Value: jsonValue(t, corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("2"),
						}),
					},
				},
			}},
		},
	}

	results, err := resourcepatch.Apply(scheme, sts, []slurmv1alpha1.ResourcePatchPolicy{policy})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Applied, "message: %s", results[0].Message)

	assert.Equal(t, "true", sts.Spec.Template.Annotations["sidecar.istio.io/inject"])
	cpu := sts.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU]
	assert.Equal(t, "2", cpu.String())

	// Identity and selector must be untouched.
	assert.Equal(t, "test-cluster-controller", sts.Name)
	require.NotNil(t, sts.Spec.Selector)
}

func TestPatchControllerStatefulSet_RejectsOwnerReferenceInjection(t *testing.T) {
	scheme := patchTestScheme(t)
	sts := renderControllerStatefulSet(t)

	policy := slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "ctl-bad", Namespace: "test-namespace"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{
				Group: "slurm.nebius.ai", Kind: "SlurmCluster", Name: "test-cluster",
			},
			Type: slurmv1alpha1.JSONPatchType,
			Patches: []slurmv1alpha1.ResourcePatch{{
				ResourceRef: slurmv1alpha1.ResourceSelector{Kind: "StatefulSet", Name: ptr.To(sts.Name)},
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{
					{Op: "add", Path: "/metadata/ownerReferences", Value: jsonValue(t, []metav1.OwnerReference{
						{APIVersion: "v1", Kind: "Pod", Name: "evil", UID: "1"},
					})},
				},
			}},
		},
	}

	results, err := resourcepatch.Apply(scheme, sts, []slurmv1alpha1.ResourcePatchPolicy{policy})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Applied)
	assert.Empty(t, sts.OwnerReferences)
}
