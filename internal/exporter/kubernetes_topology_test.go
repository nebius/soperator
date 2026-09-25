package exporter

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/internal/consts"
)

type nodeTopologySourceFunc func(context.Context) (map[string]NodeTopology, error)

func (f nodeTopologySourceFunc) ListNodeTopologies(ctx context.Context) (map[string]NodeTopology, error) {
	return f(ctx)
}

type recordingReader struct {
	client.Reader
	getKeys []client.ObjectKey
}

func (r *recordingReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	r.getKeys = append(r.getKeys, key)
	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestKubernetesNodeTopologySource(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-0",
				Namespace: "slurm",
				Labels: map[string]string{
					consts.LabelInstanceKey: "cluster-a",
					consts.LabelNodeSetKey:  "gpu-workers",
					consts.LabelWorkerKey:   consts.LabelWorkerValue,
				},
			},
			Spec: corev1.PodSpec{NodeName: "k8s-node-1"},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-1",
				Namespace: "slurm",
				Labels: map[string]string{
					consts.LabelInstanceKey: "cluster-a",
					consts.LabelNodeSetKey:  "gpu-workers",
					consts.LabelWorkerKey:   consts.LabelWorkerValue,
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-worker-0",
				Namespace: "slurm",
				Labels: map[string]string{
					consts.LabelInstanceKey: "cluster-b",
					consts.LabelNodeSetKey:  "other-workers",
					consts.LabelWorkerKey:   consts.LabelWorkerValue,
				},
			},
			Spec: corev1.PodSpec{NodeName: "k8s-node-1"},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "k8s-node-1",
				Labels: map[string]string{
					nvlinkInstanceGroupLabel:       "nvlig-1",
					"slurm.nebius.ai/nodeset-name": "infrastructure-node-pool",
				},
			},
		},
	).Build()

	source := NewKubernetesNodeTopologySource(k8sClient, "slurm", "cluster-a")
	topologies, err := source.ListNodeTopologies(context.Background())
	require.NoError(t, err)

	assert.Equal(t, map[string]NodeTopology{
		"worker-0": {
			KubernetesNode:      "k8s-node-1",
			NVLinkInstanceGroup: "nvlig-1",
			SlurmNodeSetName:    "gpu-workers",
		},
		"worker-1": {
			SlurmNodeSetName: "gpu-workers",
		},
	}, topologies)
}

func TestTopologyNodeSelector(t *testing.T) {
	selector, err := topologyNodeSelector()
	require.NoError(t, err)

	assert.True(t, selector.Matches(labels.Set{nvlinkInstanceGroupLabel: "nvlig-1"}))
	assert.True(t, selector.Matches(labels.Set{nvlinkInstanceGroupLabel: ""}))
	assert.False(t, selector.Matches(labels.Set{}))
}

func TestRegisterKubernetesNodeTopologyInformers(t *testing.T) {
	topologyCache := &informertest.FakeInformers{}

	require.NoError(t, RegisterKubernetesNodeTopologyInformers(context.Background(), topologyCache))
	assert.Contains(t, topologyCache.InformersByGVK, corev1.SchemeGroupVersion.WithKind("Pod"))
	assert.Contains(t, topologyCache.InformersByGVK, corev1.SchemeGroupVersion.WithKind("Node"))
}

func TestKubernetesNodeTopologySourceGetsEachUsedNodeOnce(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-0",
				Namespace: "slurm",
				Labels: map[string]string{
					consts.LabelInstanceKey: "cluster-a",
					consts.LabelNodeSetKey:  "gpu-workers",
					consts.LabelWorkerKey:   consts.LabelWorkerValue,
				},
			},
			Spec: corev1.PodSpec{NodeName: "k8s-node-1"},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-1",
				Namespace: "slurm",
				Labels: map[string]string{
					consts.LabelInstanceKey: "cluster-a",
					consts.LabelNodeSetKey:  "gpu-workers",
					consts.LabelWorkerKey:   consts.LabelWorkerValue,
				},
			},
			Spec: corev1.PodSpec{NodeName: "k8s-node-1"},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "k8s-node-1",
				Labels: map[string]string{
					nvlinkInstanceGroupLabel: "nvlig-1",
				},
			},
		},
	).Build()
	reader := &recordingReader{Reader: k8sClient}

	source := NewKubernetesNodeTopologySource(reader, "slurm", "cluster-a")
	topologies, err := source.ListNodeTopologies(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []client.ObjectKey{{Name: "k8s-node-1"}}, reader.getKeys)
	assert.Equal(t, map[string]NodeTopology{
		"worker-0": {
			KubernetesNode:      "k8s-node-1",
			NVLinkInstanceGroup: "nvlig-1",
			SlurmNodeSetName:    "gpu-workers",
		},
		"worker-1": {
			KubernetesNode:      "k8s-node-1",
			NVLinkInstanceGroup: "nvlig-1",
			SlurmNodeSetName:    "gpu-workers",
		},
	}, topologies)
}

func TestKubernetesNodeTopologySourceSkipsNodeReadsWithoutWorkerPods(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "k8s-node-1"}},
	).Build()
	reader := &recordingReader{Reader: k8sClient}

	source := NewKubernetesNodeTopologySource(reader, "slurm", "cluster-a")
	topologies, err := source.ListNodeTopologies(context.Background())
	require.NoError(t, err)

	assert.Empty(t, reader.getKeys)
	assert.Empty(t, topologies)
}

func TestTransformWorkerPodForTopology(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "worker-0",
			Namespace:       "slurm",
			ResourceVersion: "123",
			Labels: map[string]string{
				consts.LabelInstanceKey: "cluster-a",
				consts.LabelNodeSetKey:  "gpu-workers",
				consts.LabelWorkerKey:   consts.LabelWorkerValue,
				"irrelevant":            "value",
			},
			Annotations: map[string]string{"irrelevant": "value"},
		},
		Spec: corev1.PodSpec{
			NodeName:   "k8s-node-1",
			Containers: []corev1.Container{{Name: "slurmd"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	transformed, err := transformWorkerPodForTopology(pod)
	require.NoError(t, err)
	actual := transformed.(*corev1.Pod)

	assert.Equal(t, "worker-0", actual.Name)
	assert.Equal(t, "slurm", actual.Namespace)
	assert.Equal(t, "123", actual.ResourceVersion)
	assert.Equal(t, map[string]string{
		consts.LabelInstanceKey: "cluster-a",
		consts.LabelNodeSetKey:  "gpu-workers",
		consts.LabelWorkerKey:   consts.LabelWorkerValue,
	}, actual.Labels)
	assert.Equal(t, corev1.PodSpec{NodeName: "k8s-node-1"}, actual.Spec)
	assert.Empty(t, actual.Status)
	assert.Empty(t, actual.Annotations)
}

func TestTransformNodeForTopology(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "k8s-node-1",
			ResourceVersion: "456",
			Labels: map[string]string{
				nvlinkInstanceGroupLabel: "nvlig-1",
				"irrelevant":             "value",
			},
			Annotations: map[string]string{"irrelevant": "value"},
		},
		Spec:   corev1.NodeSpec{PodCIDR: "10.0.0.0/24"},
		Status: corev1.NodeStatus{Phase: corev1.NodeRunning},
	}

	transformed, err := transformNodeForTopology(node)
	require.NoError(t, err)
	actual := transformed.(*corev1.Node)

	assert.Equal(t, "k8s-node-1", actual.Name)
	assert.Equal(t, "456", actual.ResourceVersion)
	assert.Equal(t, map[string]string{
		nvlinkInstanceGroupLabel: "nvlig-1",
	}, actual.Labels)
	assert.Empty(t, actual.Spec)
	assert.Empty(t, actual.Status)
	assert.Empty(t, actual.Annotations)
}
