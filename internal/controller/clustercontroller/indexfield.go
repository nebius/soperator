package clustercontroller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

func indexFields(mgr ctrl.Manager) error {
	if err := indexField(mgr, consts.IndexFieldSecretSlurmKey, indexFuncSecretSlurmKey); err != nil {
		return err
	}
	if err := indexField(mgr, consts.IndexFieldSecretSSHPublicKeys, indexFuncSecretSSHPublicKeys); err != nil {
		return err
	}

	return nil
}

func indexField(mgr ctrl.Manager, field string, fn client.IndexerFunc) error {
	return mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1.SlurmCluster{}, field, fn)
}

func indexFuncSecretSlurmKey(obj client.Object) []string {
	cluster := obj.(*slurmv1.SlurmCluster)
	if cluster.Spec.Secrets.SlurmKey.Name != "" {
		return []string{cluster.Spec.Secrets.SlurmKey.Name}
	}
	return []string{}
}

func indexFuncSecretSSHPublicKeys(obj client.Object) []string {
	cluster := obj.(*slurmv1.SlurmCluster)
	if cluster.Spec.Secrets.SSHPublicKeys != nil && cluster.Spec.Secrets.SSHPublicKeys.Name != "" {
		return []string{cluster.Spec.Secrets.SSHPublicKeys.Name}
	}
	return []string{}
}
