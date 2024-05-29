package smodels

import (
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// ClusterValues contains the data to deploy and reconcile Slurm ClusterValues (Controllers and Workers)
type ClusterValues struct {
	types.NamespacedName

	Controller *ControllerValues
	Worker     *WorkerValues
}

// BuildClusterValuesFrom creates a new instance of ClusterValues given a SlurmCluster CRD
func BuildClusterValuesFrom(clusterCrd *slurmv1.SlurmCluster) (*ClusterValues, error) {
	res := &ClusterValues{
		NamespacedName: types.NamespacedName{
			Namespace: clusterCrd.Namespace,
			Name:      clusterCrd.Name,
		},
	}
	var err error

	res.Controller, err = BuildControllerValuesFrom(clusterCrd)
	if err != nil {
		return nil, err
	}

	res.Worker, err = BuildWorkerValuesFrom(clusterCrd)
	if err != nil {
		return nil, err
	}

	return res, nil
}
