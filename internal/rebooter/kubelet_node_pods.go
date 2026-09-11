package rebooter

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/kubeletclient"
)

// kubeletNodePods implements NodePodsFetcher against the kubelet of the node the rebooter
// runs on. The kubelet maps /pods onto the nodes/proxy sub-resource, so this needs the same
// RBAC grant as the API server proxy path it replaces, and it keeps the API server off the
// critical path of a drain.
type kubeletNodePods struct {
	client *kubeletclient.Client
}

// NewKubeletNodePodsFetcher creates a NodePodsFetcher that reads from a node's own kubelet.
func NewKubeletNodePodsFetcher(config *rest.Config, cfg kubeletclient.Config) (NodePodsFetcher, error) {
	// Only one kubelet is ever contacted, so a single pooled connection is enough.
	cfg.MaxIdleConns = 1

	client, err := kubeletclient.New(config, cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubelet client: %w", err)
	}
	return &kubeletNodePods{client: client}, nil
}

// GetPodsOnNode resolves the kubelet endpoint from the node's own status, so it picks up the
// port the kubelet actually advertises rather than assuming the default.
func (k *kubeletNodePods) GetPodsOnNode(ctx context.Context, node *corev1.Node) (*corev1.PodList, error) {
	address, port, err := kubeletclient.AddressForNode(node)
	if err != nil {
		return nil, err
	}

	var podList corev1.PodList
	if err := k.client.Get(ctx, address, port, kubeletclient.PodsPath, &podList); err != nil {
		return nil, fmt.Errorf("fetch pods from kubelet on node %s: %w", node.Name, err)
	}
	return &podList, nil
}

// fallbackNodePods tries primary first and falls back to secondary. The pod list gates
// draining, so a kubelet that is briefly unreachable must not stall a node reboot.
type fallbackNodePods struct {
	primary   NodePodsFetcher
	secondary NodePodsFetcher
}

func NewFallbackNodePodsFetcher(primary, secondary NodePodsFetcher) NodePodsFetcher {
	return &fallbackNodePods{primary: primary, secondary: secondary}
}

func (f *fallbackNodePods) GetPodsOnNode(ctx context.Context, node *corev1.Node) (*corev1.PodList, error) {
	podList, err := f.primary.GetPodsOnNode(ctx, node)
	if err == nil {
		return podList, nil
	}

	// Logged at Info: falling back means the direct kubelet path is broken on this node,
	// which is worth noticing even though the drain still proceeds.
	log.FromContext(ctx).WithName(ControllerName).Info(
		"Direct kubelet pod fetch failed, falling back to the API server node proxy", "error", err,
	)

	fallbackPodList, fallbackErr := f.secondary.GetPodsOnNode(ctx, node)
	if fallbackErr != nil {
		return nil, errors.Join(err, fallbackErr)
	}
	return fallbackPodList, nil
}
