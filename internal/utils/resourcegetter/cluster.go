package resourcegetter

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// GetCluster returns a [slurmv1.SlurmCluster] if it's found by types.NamespacedName.
func GetCluster(ctx context.Context, r client.Reader, name types.NamespacedName) (*slurmv1.SlurmCluster, error) {
	logger := log.FromContext(ctx)

	cluster := slurmv1.SlurmCluster{}
	if err := r.Get(ctx, name, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info(fmt.Sprintf("%s %q is not found in namespace %q", slurmv1.KindSlurmCluster, name.Name, name.Namespace))
			return nil, err
		}
		logger.Error(err, fmt.Sprintf("Failed to get %s %q in namespace %q", slurmv1.KindSlurmCluster, name.Name, name.Namespace))
		return nil, fmt.Errorf("getting %s: %w", slurmv1.KindSlurmCluster, err)
	}

	return &cluster, nil
}
