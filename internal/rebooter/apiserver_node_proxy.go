package rebooter

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NodePodsFetcher fetches the pods running on a node from the kubelet, the only authority on
// what is still running there. It never lists pods from the API server: a field-selected pod
// LIST makes the API server range-read every pod in the cluster out of etcd, and it answers
// what the control plane believes runs on the node rather than what actually does.
type NodePodsFetcher interface {
	GetPodsOnNode(ctx context.Context, node *corev1.Node) (*corev1.PodList, error)
}

// apiserverNodeProxy implements NodePodsFetcher via the API server's
// nodes/proxy sub-resource. This avoids direct kubelet access entirely -
// TLS and auth are handled by the API server using its own credentials,
// independent of kubelet serving-cert configuration.
type apiserverNodeProxy struct {
	clientset kubernetes.Interface
}

func (p *apiserverNodeProxy) GetPodsOnNode(ctx context.Context, node *corev1.Node) (*corev1.PodList, error) {
	raw, err := p.clientset.CoreV1().RESTClient().Get().
		Resource("nodes").
		Name(node.Name).
		SubResource("proxy").
		Suffix("pods").
		DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("node proxy request failed for %s: %w", node.Name, err)
	}
	var podList corev1.PodList
	if err := json.Unmarshal(raw, &podList); err != nil {
		return nil, fmt.Errorf("decode proxy response for %s: %w", node.Name, err)
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
