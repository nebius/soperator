package rebooter

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NodePodsFetcher fetches the pods running on a specific node without going
// through the controller-runtime informer cache.
type NodePodsFetcher interface {
	GetPodsOnNode(ctx context.Context, nodeName string) (*corev1.PodList, error)
}

// apiserverNodeProxy implements NodePodsFetcher via the API server's
// nodes/proxy sub-resource. This avoids direct kubelet access entirely -
// TLS and auth are handled by the API server using its own credentials,
// independent of kubelet serving-cert configuration.
type apiserverNodeProxy struct {
	clientset kubernetes.Interface
}

func (p *apiserverNodeProxy) GetPodsOnNode(ctx context.Context, nodeName string) (*corev1.PodList, error) {
	raw, err := p.clientset.CoreV1().RESTClient().Get().
		Resource("nodes").
		Name(nodeName).
		SubResource("proxy").
		Suffix("pods").
		DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("node proxy request failed for %s: %w", nodeName, err)
	}
	var podList corev1.PodList
	if err := json.Unmarshal(raw, &podList); err != nil {
		return nil, fmt.Errorf("decode proxy response for %s: %w", nodeName, err)
	}
	return &podList, nil
}

// NewAPIServerNodePodsFetcher creates a NodePodsFetcher that proxies through the API server.
func NewAPIServerNodePodsFetcher(config *rest.Config) (NodePodsFetcher, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return &apiserverNodeProxy{clientset: clientset}, nil
}
