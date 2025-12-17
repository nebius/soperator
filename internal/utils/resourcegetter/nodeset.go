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
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

// ListNodeSetsByClusterRef returns a list of [slurmv1alpha1.NodeSet] that have consts.AnnotationParentalClusterRefName annotation with clusterRef.Name value.
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
			clusterName, found := nodeSet.GetAnnotations()[consts.AnnotationParentalClusterRefName]
			if found && clusterName == clusterRef.Name {
				return true
			}
			return false
		}),
		func(a, b slurmv1alpha1.NodeSet) int {
			return strings.Compare(a.Name, b.Name)
		},
	), nil
}
