package resourcegetter

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// GetClusterInNamespace returns a [slurmv1.SlurmCluster] if it's found in the namespace.
// It assumes that there may be no or just one cluster in the namespace.
func GetClusterInNamespace(ctx context.Context, r client.Reader, namespace string) (*slurmv1.SlurmCluster, error) {
	logger := log.FromContext(ctx)

	clusterList := slurmv1.SlurmClusterList{}
	if err := r.List(ctx, &clusterList, client.InNamespace(namespace)); err != nil {
		logger.Error(err, fmt.Sprintf("Failed to list %s in namespace %q", slurmv1.KindSlurmCluster, namespace))
		return nil, fmt.Errorf("listing %s: %w", slurmv1.KindSlurmCluster, err)
	}

	if len(clusterList.Items) == 0 {
		logger.V(1).Info(fmt.Sprintf("No %s resources found in namespace %q", slurmv1.KindSlurmCluster, namespace))
		return nil, nil
	}

	if len(clusterList.Items) > 1 {
		err := errors.New(fmt.Sprintf("multiple %s resources found in namespace %q", slurmv1.KindSlurmCluster, namespace))
		logger.Error(err, fmt.Sprintf("%d %s resources found in namespace %q. This should not happen and definitely is a bug", len(clusterList.Items), slurmv1.KindSlurmCluster, namespace))
		return nil, err
	}

	return clusterList.Items[0].DeepCopy(), nil
}

// GetCluster returns a [slurmv1.SlurmCluster] if it's found by types.NamespacedName.
func GetCluster(ctx context.Context, r client.Reader, name types.NamespacedName) (*slurmv1.SlurmCluster, error) {
	logger := log.FromContext(ctx)

	cluster := slurmv1.SlurmCluster{}
	if err := r.Get(ctx, name, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info(fmt.Sprintf("%s %q is not found in namespace %q", slurmv1.KindSlurmCluster, name.Name, name.Namespace))
			return nil, nil
		}
		logger.Error(err, fmt.Sprintf("Failed to get %s %q in namespace %q", slurmv1.KindSlurmCluster, name.Name, name.Namespace))
		return nil, fmt.Errorf("getting %s: %w", slurmv1.KindSlurmCluster, err)
	}

	return &cluster, nil
}
