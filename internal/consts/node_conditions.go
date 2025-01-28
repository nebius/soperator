package consts

import corev1 "k8s.io/api/core/v1"

const (
	SlurmNodeDrain  = corev1.NodeConditionType("SlurmNodeDrain")
	SlurmNodeReboot = corev1.NodeConditionType("SlurmNodeReboot")
)
