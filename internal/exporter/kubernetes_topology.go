package exporter

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/rest"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
)

type kubernetesNodeTopologySource struct {
	reader      client.Reader
	namespace   string
	clusterName string
}

func topologyNodeSelector() (labels.Selector, error) {
	nvlinkRequirement, err := labels.NewRequirement(nvlinkInstanceGroupLabel, selection.Exists, nil)
	if err != nil {
		return nil, fmt.Errorf("create NVLink instance group label selector: %w", err)
	}
	return labels.NewSelector().Add(*nvlinkRequirement), nil
}

// NewKubernetesNodeTopologyCache creates a list/watch cache scoped to the Kubernetes objects
// needed to resolve Slurm worker topology.
func NewKubernetesNodeTopologyCache(
	cfg *rest.Config,
	namespace, clusterName string,
) (ctrlcache.Cache, error) {
	workerSelector := labels.SelectorFromSet(labels.Set{
		consts.LabelInstanceKey: clusterName,
		consts.LabelWorkerKey:   consts.LabelWorkerValue,
	})
	nodeSelector, err := topologyNodeSelector()
	if err != nil {
		return nil, err
	}

	topologyCache, err := ctrlcache.New(cfg, ctrlcache.Options{
		ByObject: map[client.Object]ctrlcache.ByObject{
			&corev1.Pod{}: {
				Namespaces: map[string]ctrlcache.Config{
					namespace: {},
				},
				Label:     workerSelector,
				Transform: transformWorkerPodForTopology,
			},
			&corev1.Node{}: {
				Label:     nodeSelector,
				Transform: transformNodeForTopology,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes node topology cache: %w", err)
	}

	return topologyCache, nil
}

// RegisterKubernetesNodeTopologyInformers creates the Pod and Node informers before the cache starts.
func RegisterKubernetesNodeTopologyInformers(ctx context.Context, topologyCache ctrlcache.Cache) error {
	for _, obj := range []client.Object{&corev1.Pod{}, &corev1.Node{}} {
		if _, err := topologyCache.GetInformer(ctx, obj, ctrlcache.BlockUntilSynced(false)); err != nil {
			return fmt.Errorf("register Kubernetes topology informer for %T: %w", obj, err)
		}
	}
	return nil
}

// NewKubernetesNodeTopologySource creates a topology source backed by worker Pods and Nodes.
func NewKubernetesNodeTopologySource(
	reader client.Reader,
	namespace, clusterName string,
) NodeTopologySource {
	if reader == nil {
		return nil
	}
	return &kubernetesNodeTopologySource{
		reader:      reader,
		namespace:   namespace,
		clusterName: clusterName,
	}
}

func (s *kubernetesNodeTopologySource) ListNodeTopologies(ctx context.Context) (map[string]NodeTopology, error) {
	var pods corev1.PodList
	if err := s.reader.List(
		ctx,
		&pods,
		client.InNamespace(s.namespace),
		client.MatchingLabels{
			consts.LabelInstanceKey: s.clusterName,
			consts.LabelWorkerKey:   consts.LabelWorkerValue,
		},
	); err != nil {
		return nil, fmt.Errorf("list Slurm worker pods: %w", err)
	}

	topologies := make(map[string]NodeTopology, len(pods.Items))
	podsByKubernetesNode := make(map[string][]string, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		topologies[pod.Name] = NodeTopology{
			KubernetesNode:   pod.Spec.NodeName,
			SlurmNodeSetName: pod.Labels[consts.LabelNodeSetKey],
		}
		if pod.Spec.NodeName == "" {
			continue
		}
		podsByKubernetesNode[pod.Spec.NodeName] = append(podsByKubernetesNode[pod.Spec.NodeName], pod.Name)
	}

	for kubernetesNode, podNames := range podsByKubernetesNode {
		var node corev1.Node
		if err := s.reader.Get(ctx, client.ObjectKey{Name: kubernetesNode}, &node); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("get Kubernetes node %q: %w", kubernetesNode, err)
		}

		for _, podName := range podNames {
			topology := topologies[podName]
			topology.NVLinkInstanceGroup = node.Labels[nvlinkInstanceGroupLabel]
			topologies[podName] = topology
		}
	}

	return topologies, nil
}

func transformWorkerPodForTopology(obj any) (any, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}

	pod.ObjectMeta = metav1.ObjectMeta{
		Name:            pod.Name,
		Namespace:       pod.Namespace,
		UID:             pod.UID,
		ResourceVersion: pod.ResourceVersion,
		Labels: map[string]string{
			consts.LabelInstanceKey: pod.Labels[consts.LabelInstanceKey],
			consts.LabelNodeSetKey:  pod.Labels[consts.LabelNodeSetKey],
			consts.LabelWorkerKey:   pod.Labels[consts.LabelWorkerKey],
		},
	}
	pod.Spec = corev1.PodSpec{NodeName: pod.Spec.NodeName}
	pod.Status = corev1.PodStatus{}

	return pod, nil
}

func transformNodeForTopology(obj any) (any, error) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return obj, nil
	}

	node.ObjectMeta = metav1.ObjectMeta{
		Name:            node.Name,
		UID:             node.UID,
		ResourceVersion: node.ResourceVersion,
		Labels: map[string]string{
			nvlinkInstanceGroupLabel: node.Labels[nvlinkInstanceGroupLabel],
		},
	}
	node.Spec = corev1.NodeSpec{}
	node.Status = corev1.NodeStatus{}

	return node, nil
}
