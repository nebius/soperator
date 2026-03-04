package resourcegetter

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

// ListNodeSetsByClusterRef returns a list of [slurmv1alpha1.NodeSet] whose spec.slurmClusterRefName matches clusterRef.Name.
func ListNodeSetsByClusterRef(ctx context.Context, r client.Reader, clusterRef types.NamespacedName) ([]slurmv1alpha1.NodeSet, error) {
	logger := log.FromContext(ctx)

	nodeSetList := slurmv1alpha1.NodeSetList{}
	if err := r.List(ctx, &nodeSetList,
		client.InNamespace(clusterRef.Namespace),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Failed to list %s in namespace %q", slurmv1alpha1.KindNodeSet, clusterRef.Namespace))
		return nil, fmt.Errorf("listing %s: %w", slurmv1alpha1.KindNodeSet, err)
	}

	return slices.SortedFunc(
		sliceutils.FilterSliceSeq(nodeSetList.Items, func(nodeSet slurmv1alpha1.NodeSet) bool {
			return nodeSet.Spec.SlurmClusterRefName == clusterRef.Name
		}),
		func(a, b slurmv1alpha1.NodeSet) int {
			return strings.Compare(a.Name, b.Name)
		},
	), nil
}
