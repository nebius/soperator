package smodels

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/models/k8s"
)

// ControllerValues contains the data needed to deploy and reconcile the Slurm Controllers
type ControllerValues struct {
	k8smodels.Resourceful

	Service     k8smodels.Service
	StatefulSet k8smodels.StatefulSet
	SpoolPV     k8smodels.PV
	SpoolPVC    k8smodels.PVC
	Image       k8smodels.Image
}

func BuildControllerValuesFrom(clusterCrd *slurmv1.SlurmCluster) (*ControllerValues, error) {
	namespace := clusterCrd.Namespace
	clusterName := clusterCrd.Name

	resourceful, err := k8smodels.BuildResourcefulFrom(clusterCrd.Spec.ControllerNode.Pod)
	if err != nil {
		return nil, err
	}

	service, err := k8smodels.BuildServiceFrom(namespace, clusterName, consts.ComponentTypeController)
	if err != nil {
		return nil, err
	}

	statefulSet := k8smodels.BuildStatefulSetFrom(namespace, clusterName, consts.ComponentTypeController, clusterCrd.Spec.ControllerNode)

	spoolPVC, err := k8smodels.BuildPVCFrom(namespace, clusterName, consts.ComponentTypeController, clusterCrd.Spec.ControllerNode.Pod)
	if err != nil {
		return nil, err
	}

	image, err := k8smodels.BuildImageFrom(clusterCrd.Spec.ControllerNode.Image, consts.ComponentTypeController)
	if err != nil {
		return nil, err
	}

	// TODO fill other values

	return &ControllerValues{
		Resourceful: resourceful,
		Service:     service,
		StatefulSet: statefulSet,
		SpoolPVC:    spoolPVC,
		Image:       image,
	}, nil
}
