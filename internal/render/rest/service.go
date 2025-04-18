package rest

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderService renders new [corev1.Service] serving Slurm REST API
func RenderService(namespace, clusterName string, rest *values.SlurmREST) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rest.Service.Name,
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeREST, clusterName),
		},
		Spec: corev1.ServiceSpec{
			Type:      rest.Service.Type,
			Selector:  common.RenderMatchLabels(consts.ComponentTypeREST, clusterName),
			ClusterIP: "",
			Ports: []corev1.ServicePort{{
				Protocol:   rest.Service.Protocol,
				Port:       rest.ContainerREST.Port,
				TargetPort: intstr.FromString(rest.ContainerREST.Name),
			}},
		},
	}
}

func GetServiceURL(namespace string, rest *values.SlurmREST) string {
	return fmt.Sprintf("http://%s.%s.svc:%d", rest.Service.Name, namespace, rest.ContainerREST.Port)
}
