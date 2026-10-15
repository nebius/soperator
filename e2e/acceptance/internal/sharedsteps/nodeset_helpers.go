package sharedsteps

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func listClusterNodeSets(
	ctx context.Context,
	kubectl *framework.KubectlClient,
	clusterName string,
) ([]kubeobjects.NodeSet, error) {
	var nodeSets kubeobjects.NodeSetList
	if err := kubectl.GetJSON(ctx, &nodeSets,
		"get", "nodesets", "-n", framework.SoperatorNamespace, "-o", "json",
	); err != nil {
		return nil, fmt.Errorf("list NodeSets: %w", err)
	}

	filtered := make([]kubeobjects.NodeSet, 0, len(nodeSets.Items))
	for _, nodeSet := range nodeSets.Items {
		if clusterName != "" && nodeSet.Spec.ClusterName != "" && nodeSet.Spec.ClusterName != clusterName {
			continue
		}
		filtered = append(filtered, nodeSet)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Metadata.Namespace == filtered[j].Metadata.Namespace {
			return filtered[i].Metadata.Name < filtered[j].Metadata.Name
		}
		return filtered[i].Metadata.Namespace < filtered[j].Metadata.Namespace
	})
	return filtered, nil
}

func nodeSetWorkerPods(
	ctx context.Context,
	kubectl *framework.KubectlClient,
	nodeSet kubeobjects.NodeSet,
) ([]corev1.Pod, error) {
	if nodeSet.Metadata.Name == "" {
		return nil, fmt.Errorf("list worker pods: NodeSet name is empty")
	}

	return kubectl.NodeSetWorkerPods(ctx, nodeSet.Metadata.Namespace, nodeSet.Metadata.Name)
}
