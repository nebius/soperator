package accounting

import (
	"errors"

	mariadv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	consts "nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderMariaDb(
	namespace,
	clusterName string,
	accounting *values.SlurmAccounting,
	nodeFilters []slurmv1.K8sNodeFilter,
) (*mariadv1alpha1.MariaDB, error) {

	if !accounting.MariaDb.Enabled {
		return nil, errors.New("MariaDb is not enabled")
	}

	mariaDb := accounting.MariaDb
	labels := common.RenderLabels(consts.ComponentTypeMariaDbOperator, clusterName)
	port, replicas, antiAffinityEnabled := getMariaDbConfig(mariaDb)

	nodeFilter, err := utils.GetBy(
		nodeFilters,
		accounting.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err != nil {
		return nil, err
	}

	affinityConfig := getAffinityConfig(nodeFilter.Affinity, antiAffinityEnabled)

	return &mariadv1alpha1.MariaDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildMariaDbName(clusterName),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: mariadv1alpha1.MariaDBSpec{
			Image:    mariaDb.Image,
			Replicas: replicas,
			Port:     port,
			Storage:  mariaDb.Storage,
			Database: ptr.To(consts.MariaDbDatabase),
			Username: ptr.To(consts.MariaDbUsername),
			PasswordSecretKeyRef: &mariadv1alpha1.GeneratedSecretKeyRef{
				SecretKeySelector: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: consts.MariaDbSecretName,
					},
					Key: consts.MariaDbPasswordKey,
				},
				Generate: true,
			},
			RootEmptyPassword: ptr.To(false),
			RootPasswordSecretKeyRef: mariadv1alpha1.GeneratedSecretKeyRef{
				SecretKeySelector: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: consts.MariaDbSecretRootName,
					},
					Key: consts.MariaDbPasswordKey,
				},
				Generate: true,
			},
			Service: &mariadv1alpha1.ServiceTemplate{
				Type: corev1.ServiceTypeClusterIP,
			},
			ContainerTemplate: mariadv1alpha1.ContainerTemplate{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: *mariaDb.Resources.Memory(),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: *mariaDb.Resources.Memory(),
						corev1.ResourceCPU:    *mariaDb.Resources.Cpu(),
					},
				},
				SecurityContext: mariaDb.SecurityContext,
			},
			PodTemplate: mariadv1alpha1.PodTemplate{
				NodeSelector:       nodeFilter.NodeSelector,
				Affinity:           affinityConfig,
				PodSecurityContext: mariaDb.PodSecurityContext,

			},
			Metrics: mariaDb.Metrics,
			MyCnf:   ptr.To(consts.MariaDbDefaultMyCnf),
		},
	}, nil
}

func getMariaDbConfig(mariaDb slurmv1.MariaDbOperator) (int32, int32, *bool) {
	port := int32(consts.MariaDbPort)
	replicas := int32(1)
	antiAffinityEnabled := ptr.To(false)

	if mariaDb.Port != 0 {
		port = mariaDb.Port
	}
	if mariaDb.Replicas != 0 {
		replicas = mariaDb.Replicas
	}
	if replicas > 1 {
		antiAffinityEnabled = ptr.To(true)
	}

	return port, replicas, antiAffinityEnabled
}

func getAffinityConfig(affinity *corev1.Affinity, antiAffinityEnabled *bool) *mariadv1alpha1.AffinityConfig {
	affinityConfig := &mariadv1alpha1.AffinityConfig{
		AntiAffinityEnabled: antiAffinityEnabled,
	}

	if affinity != nil {
		switch {
		case affinity.NodeAffinity != nil:
			affinityConfig.NodeAffinity = affinity.NodeAffinity
		case affinity.PodAffinity != nil:
			affinityConfig.PodAffinity = affinity.PodAffinity
		case affinity.PodAntiAffinity != nil:
			affinityConfig.PodAntiAffinity = affinity.PodAntiAffinity
		}
	}

	return affinityConfig
}
