package resourcegetter

import (
	"context"
	"fmt"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
)

// ResolveWorkloadNamePrefix returns the prefix to use for workload resource names
//
// Detection is done by looking up the controller StatefulSet under its legacy unprefixed name.
// If it exists and belongs to this cluster, the cluster predates prefixed naming — "" is returned
// so all workload resources continue to be managed under their original names without any rename or deletion.
// For new clusters clusterName is returned, giving every workload resource a <clusterName>-<component> name.
func ResolveWorkloadNamePrefix(
	ctx context.Context,
	r client.Reader,
	namespace,
	clusterName string,
) (string, error) {
	sts := &kruisev1b1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      consts.ComponentTypeController.String(),
	}, sts)

	if err == nil && hasClusterLabel(sts, clusterName) {
		// Legacy cluster: unprefixed StatefulSet already exists and belongs to this cluster.
		return "", nil
	}

	if err != nil && !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("checking legacy controller StatefulSet: %w", err)
	}

	return clusterName, nil
}

// hasClusterLabel checks whether the resource carries the standard instance label for the given cluster.
func hasClusterLabel(obj client.Object, clusterName string) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	return labels[consts.LabelInstanceKey] == clusterName
}

func BuildPrefixedName(prefix, base string) string {
	if prefix == "" {
		return base
	}
	return prefix + "-" + base
}
