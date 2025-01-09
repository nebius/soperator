package accounting

import (
	"errors"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
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
) (*mariadbv1alpha1.MariaDB, error) {

	if !accounting.MariaDb.Enabled {
		return nil, errors.New("MariaDb is not enabled")
	}

	mariaDb := accounting.MariaDb
	labels := common.RenderLabels(consts.ComponentTypeMariaDbOperator, clusterName)
	port, replicas, antiAffinityEnabled := getMariaDbConfig(mariaDb)

	if check.IsMaintenanceActive(accounting.Maintenance) {
		replicas = consts.ZeroReplicas
	}

	nodeFilter, err := utils.GetBy(
		nodeFilters,
		accounting.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err != nil {
		return nil, err
	}

	affinityConfig := getAffinityConfig(nodeFilter.Affinity, antiAffinityEnabled)
	// Create a copy of the container's limits and add non-CPU resources from Requests
	limits := common.CopyNonCPUResources(mariaDb.Resources)

	// If the MariaDB secret is protected, the mariadb-operator will not generate the secret
	// it will be the soperator's responsibility to generate the secret
	generateSecret := func(isProtected bool) bool { return !isProtected }(mariaDb.ProtectedSecret)

	return &mariadbv1alpha1.MariaDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildMariaDbName(clusterName),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: mariadbv1alpha1.MariaDBSpec{
			Image:    mariaDb.Image,
			Replicas: replicas,
			Port:     port,
			Storage:  mariaDb.Storage,
			Database: ptr.To(consts.MariaDbDatabase),
			Username: ptr.To(consts.MariaDbUsername),
			PasswordSecretKeyRef: &mariadbv1alpha1.GeneratedSecretKeyRef{
				SecretKeySelector: mariadbv1alpha1.SecretKeySelector{
					LocalObjectReference: mariadbv1alpha1.LocalObjectReference{
						Name: consts.MariaDbSecretName,
					},
					Key: consts.MariaDbPasswordKey,
				},
				Generate: generateSecret,
			},
			RootEmptyPassword: ptr.To(false),
			RootPasswordSecretKeyRef: mariadbv1alpha1.GeneratedSecretKeyRef{
				SecretKeySelector: mariadbv1alpha1.SecretKeySelector{
					LocalObjectReference: mariadbv1alpha1.LocalObjectReference{
						Name: consts.MariaDbSecretRootName,
					},
					Key: consts.MariaDbPasswordKey,
				},
				Generate: generateSecret,
			},
			Service: &mariadbv1alpha1.ServiceTemplate{
				Type: corev1.ServiceTypeClusterIP,
			},
			ContainerTemplate: mariadbv1alpha1.ContainerTemplate{
				Resources: &mariadbv1alpha1.ResourceRequirements{
					Limits: limits,
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: *mariaDb.Resources.Memory(),
						corev1.ResourceCPU:    *mariaDb.Resources.Cpu(),
					},
				},
				SecurityContext: mariaDb.SecurityContext,
			},
			PodTemplate: mariadbv1alpha1.PodTemplate{
				NodeSelector:       nodeFilter.NodeSelector,
				Affinity:           affinityConfig,
				Tolerations:        nodeFilter.Tolerations,
				PodSecurityContext: mariaDb.PodSecurityContext,
			},
			Metrics: &mariadbv1alpha1.MariadbMetrics{
				Enabled: mariaDb.Metrics.Enabled,
			},
			MyCnf: ptr.To(consts.MariaDbDefaultMyCnf),
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

func getAffinityConfig(affinity *corev1.Affinity, antiAffinityEnabled *bool) *mariadbv1alpha1.AffinityConfig {
	affinityConfig := &mariadbv1alpha1.AffinityConfig{
		AntiAffinityEnabled: antiAffinityEnabled,
	}

	if affinity != nil {
		switch {
		case affinity.NodeAffinity != nil:
			affinityConfig.NodeAffinity = ConvertCoreV1ToMariaDBV1Alpha1NodeAffinity(affinity.NodeAffinity)
		case affinity.PodAntiAffinity != nil:
			affinityConfig.PodAntiAffinity = ConvertCoreV1ToMariaDBV1Alpha1PodAntiAffinity(affinity.PodAntiAffinity)
		}
	}

	return affinityConfig
}

// ConvertCoreV1ToMariaDBV1Alpha1PodAntiAffinity converts *corev1.PodAntiAffinity to *mariadbv1alpha1.PodAntiAffinity
func ConvertCoreV1ToMariaDBV1Alpha1PodAntiAffinity(corePodAntiAffinity *corev1.PodAntiAffinity) *mariadbv1alpha1.PodAntiAffinity {
	if corePodAntiAffinity == nil {
		return nil
	}

	mariadbPodAntiAffinity := &mariadbv1alpha1.PodAntiAffinity{}

	// Convert PreferredDuringSchedulingIgnoredDuringExecution
	for _, preferred := range corePodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
		mariadbPodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
			mariadbPodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			mariadbv1alpha1.WeightedPodAffinityTerm{
				Weight: preferred.Weight,
				PodAffinityTerm: mariadbv1alpha1.PodAffinityTerm{
					LabelSelector: ConvertLabelSelector(preferred.PodAffinityTerm.LabelSelector),
					TopologyKey:   preferred.PodAffinityTerm.TopologyKey, // Exclude Namespaces
				},
			},
		)
	}

	// Convert RequiredDuringSchedulingIgnoredDuringExecution
	for _, required := range corePodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
		mariadbPodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(
			mariadbPodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			mariadbv1alpha1.PodAffinityTerm{
				LabelSelector: ConvertLabelSelector(required.LabelSelector),
				TopologyKey:   required.TopologyKey, // Exclude Namespaces
			},
		)
	}

	return mariadbPodAntiAffinity
}

// ConvertLabelSelector converts *metav1.LabelSelector to *mariadbv1alpha1.LabelSelector
func ConvertLabelSelector(selector *metav1.LabelSelector) *mariadbv1alpha1.LabelSelector {
	if selector == nil {
		return nil
	}

	return &mariadbv1alpha1.LabelSelector{
		MatchLabels:      selector.MatchLabels,
		MatchExpressions: ConvertLabelSelectorRequirements(selector.MatchExpressions),
	}
}

// ConvertLabelSelectorRequirements converts []metav1.LabelSelectorRequirement to []mariadbv1alpha1.LabelSelectorRequirement
func ConvertLabelSelectorRequirements(requirements []metav1.LabelSelectorRequirement) []mariadbv1alpha1.LabelSelectorRequirement {
	if requirements == nil {
		return nil
	}

	var convertedRequirements []mariadbv1alpha1.LabelSelectorRequirement
	for _, req := range requirements {
		convertedRequirements = append(convertedRequirements, mariadbv1alpha1.LabelSelectorRequirement{
			Key:      req.Key,
			Operator: req.Operator, // Directly assign req.Operator
			Values:   req.Values,
		})
	}

	return convertedRequirements
}

// ConvertCoreV1ToMariaDBV1Alpha1NodeAffinity converts *corev1.NodeAffinity to *mariadbv1alpha1.NodeAffinity
func ConvertCoreV1ToMariaDBV1Alpha1NodeAffinity(coreNodeAffinity *corev1.NodeAffinity) *mariadbv1alpha1.NodeAffinity {
	if coreNodeAffinity == nil {
		return nil
	}

	return &mariadbv1alpha1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution:  ConvertNodeSelector(coreNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution),
		PreferredDuringSchedulingIgnoredDuringExecution: ConvertPreferredSchedulingTerms(coreNodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution),
	}
}

// ConvertNodeSelector converts *corev1.NodeSelector to *mariadbv1alpha1.NodeSelector
func ConvertNodeSelector(coreNodeSelector *corev1.NodeSelector) *mariadbv1alpha1.NodeSelector {
	if coreNodeSelector == nil {
		return nil
	}

	var terms []mariadbv1alpha1.NodeSelectorTerm
	for _, term := range coreNodeSelector.NodeSelectorTerms {
		terms = append(terms, ConvertNodeSelectorTerm(term))
	}

	return &mariadbv1alpha1.NodeSelector{
		NodeSelectorTerms: terms,
	}
}

// ConvertNodeSelectorTerm converts corev1.NodeSelectorTerm to mariadbv1alpha1.NodeSelectorTerm
func ConvertNodeSelectorTerm(term corev1.NodeSelectorTerm) mariadbv1alpha1.NodeSelectorTerm {
	return mariadbv1alpha1.NodeSelectorTerm{
		MatchExpressions: ConvertNodeSelectorRequirements(term.MatchExpressions),
		MatchFields:      ConvertNodeSelectorRequirements(term.MatchFields),
	}
}

// ConvertNodeSelectorRequirements converts []corev1.NodeSelectorRequirement to []mariadbv1alpha1.NodeSelectorRequirement
func ConvertNodeSelectorRequirements(reqs []corev1.NodeSelectorRequirement) []mariadbv1alpha1.NodeSelectorRequirement {
	var converted []mariadbv1alpha1.NodeSelectorRequirement
	for _, req := range reqs {
		converted = append(converted, mariadbv1alpha1.NodeSelectorRequirement{
			Key:      req.Key,
			Operator: req.Operator, // Convert Operator to string
			Values:   req.Values,
		})
	}
	return converted
}

// ConvertPreferredSchedulingTerms converts []corev1.PreferredSchedulingTerm to []mariadbv1alpha1.PreferredSchedulingTerm
func ConvertPreferredSchedulingTerms(terms []corev1.PreferredSchedulingTerm) []mariadbv1alpha1.PreferredSchedulingTerm {
	var converted []mariadbv1alpha1.PreferredSchedulingTerm
	for _, term := range terms {
		converted = append(converted, mariadbv1alpha1.PreferredSchedulingTerm{
			Weight:     term.Weight,
			Preference: ConvertNodeSelectorTerm(term.Preference),
		})
	}
	return converted
}
