package k8smodels

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/models/k8s/naming"
)

type Service struct {
	types.NamespacedName

	Ports []corev1.ServicePort
}

func BuildServiceFrom(namespace, clusterName string, componentType consts.ComponentType) (Service, error) {
	res := Service{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      k8snaming.BuildServiceName(clusterName, componentType),
		},
	}

	switch componentType {
	case consts.ComponentTypeController:
		res.Ports = []corev1.ServicePort{{
			Protocol:   consts.ServiceControllerClusterPortProtocol,
			Port:       consts.ServiceControllerClusterPort,
			TargetPort: intstr.FromString(consts.ServiceControllerClusterTargetPort),
		}}
	case consts.ComponentTypeWorker:
		res.Ports = []corev1.ServicePort{{
			Protocol:   consts.ServiceWorkerClusterPortProtocol,
			Port:       consts.ServiceWorkerClusterPort,
			TargetPort: intstr.FromString(consts.ServiceWorkerClusterTargetPort),
		}}
	default:
		return Service{}, fmt.Errorf("failed to get service ports for unknown component type %q", componentType)
	}

	return res, nil
}
