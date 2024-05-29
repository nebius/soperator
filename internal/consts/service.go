package consts

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	ServiceControllerClusterPortProtocol = corev1.ProtocolTCP
	ServiceControllerClusterPort         = 6817
	ServiceControllerClusterTargetPort   = "slurmctld"

	ServiceWorkerClusterPortProtocol = corev1.ProtocolTCP
	ServiceWorkerClusterPort         = 6818
	ServiceWorkerClusterTargetPort   = "slurmd"
)
