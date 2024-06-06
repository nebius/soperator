package clustercontroller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

func indexFields(mgr ctrl.Manager) error {
	if err := indexField(mgr, consts.IndexFieldSecretMungeKey, indexFuncSecretMungeKey); err != nil {
		return err
	}
	if err := indexField(mgr, consts.IndexFieldSecretSSHRootPublicKeys, indexFuncSecretSSHRootPublicKeys); err != nil {
		return err
	}

	return nil
}

func indexField(mgr ctrl.Manager, field string, fn client.IndexerFunc) error {
	return mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1.SlurmCluster{}, field, fn)
}

func indexFuncSecretMungeKey(obj client.Object) []string {
	cluster := obj.(*slurmv1.SlurmCluster)
	if cluster.Spec.Secrets.MungeKey.Name != "" {
		return []string{cluster.Spec.Secrets.MungeKey.Name}
	}
	return []string{}
}

func indexFuncSecretSSHRootPublicKeys(obj client.Object) []string {
	cluster := obj.(*slurmv1.SlurmCluster)
	if cluster.Spec.Secrets.SSHRootPublicKeys != nil && cluster.Spec.Secrets.SSHRootPublicKeys.Name != "" {
		return []string{cluster.Spec.Secrets.SSHRootPublicKeys.Name}
	}
	return []string{}
}
