package k8smodels

import (
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/models/k8s/naming"
)

type StatefulSet struct {
	types.NamespacedName

	Replicas       int32
	MaxUnavailable intstr.IntOrString
}

func BuildStatefulSetFrom(
	namespace,
	clusterName string,
	componentType consts.ComponentType,
	controllerSpec slurmv1.ControllerNodeSpec,
) StatefulSet {
	return StatefulSet{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      k8snaming.BuildStatefulSetName(clusterName, componentType),
		},
		Replicas:       controllerSpec.Size,
		MaxUnavailable: intstr.FromInt32(1),
	}
}
