package worker

import (
	"fmt"
	"maps"
	"slices"
	"sort"

	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderNodeSetStatefulSet renders new [kruisev1b1.StatefulSet] containing NodeSet worker pods
func RenderNodeSetStatefulSet(
	clusterName string,
	nodeSet *values.SlurmNodeSet,
	secrets *slurmv1.Secrets,
	cgroupVersion string,
	topologyPluginEnabled bool,
) (kruisev1b1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeNodeSet, nodeSet.ParentalCluster.Name)
	labels[consts.LabelNodeSetKey] = nodeSet.Name
	labels[consts.LabelWorkerKey] = consts.LabelWorkerValue
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeNodeSet, nodeSet.ParentalCluster.Name)
	matchLabels[consts.LabelNodeSetKey] = nodeSet.Name

	volumes, pvcTemplateSpecs, err := renderVolumesAndClaimTemplateSpecsForNodeSet(nodeSet, secrets)
	if err != nil {
		return kruisev1b1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	topologyTimeOut := nodeSet.EphemeralTopologyWaitTimeout
	if topologyTimeOut == 0 {
		topologyTimeOut = consts.DefaultEphemeralTopologyWaitTimeout
	}

	isNodeSet := true

	initContainers := slices.Clone(nodeSet.CustomInitContainers)
	initContainers = append(initContainers,
		common.RenderContainerMunge(&nodeSet.ContainerMunge),
		RenderContainerWorkerInit(clusterName, &nodeSet.ContainerSlurmd, topologyPluginEnabled, isNodeSet, topologyTimeOut),
	)

	if topologyPluginEnabled {
		volumes = append(volumes,
			corev1.Volume{
				Name: consts.VolumeNameTopologyNodeLabels,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: consts.ConfigMapNameTopologyNodeLabels,
						},
						Optional: ptr.To(true),
					},
				},
			},
		)
	}

	slurmdContainer, err := renderContainerNodeSetSlurmd(nodeSet, topologyPluginEnabled, cgroupVersion)
	if err != nil {
		return kruisev1b1.StatefulSet{}, fmt.Errorf("rendering slurmd container: %w", err)
	}

	replicas := &nodeSet.StatefulSet.Replicas
	var reserveOrdinals []intstr.IntOrString

	if check.IsMaintenanceActive(nodeSet.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	} else if isEphemeralNodesEnabled(nodeSet) {
		// For ephemeral nodes, replicas and reserveOrdinals are calculated based on activeNodes.
		// If activeNodes is empty, no pods will be created (replicas = 0).
		// Pods are only created when NodeSetPowerState.spec.activeNodes is populated by the power-manager.
		replicas, reserveOrdinals = calculateReplicasAndReserveOrdinals(nodeSet.ActiveNodes)
	}

	spec := corev1.PodSpec{
		HostUsers: &nodeSet.EnableHostUserNamespace,
		ReadinessGates: []corev1.PodReadinessGate{
			{
				ConditionType: appspub.InPlaceUpdateReady,
			},
		},
		PriorityClassName:  nodeSet.PriorityClass,
		ServiceAccountName: naming.BuildServiceAccountWorkerName(nodeSet.ParentalCluster.Name),
		Affinity:           nodeSet.Affinity,
		NodeSelector:       nodeSet.NodeSelector,
		Tolerations:        nodeSet.Tolerations,
		InitContainers:     initContainers,
		Containers: []corev1.Container{
			slurmdContainer,
		},
		Volumes:   volumes,
		Subdomain: nodeSet.ServiceUmbrella.Name,
		DNSPolicy: corev1.DNSClusterFirst,
		DNSConfig: &corev1.PodDNSConfig{
			Searches: []string{
				naming.BuildServiceFQDN(nodeSet.ServiceUmbrella.Name, nodeSet.ParentalCluster.Namespace),
				naming.BuildLoginHeadlessServiceFQDN(nodeSet.ParentalCluster.Namespace, nodeSet.ParentalCluster.Name),
			},
		},
		RestartPolicy:                 corev1.RestartPolicyAlways,
		TerminationGracePeriodSeconds: ptr.To(common.DefaultPodTerminationGracePeriodSeconds),
		SecurityContext:               &corev1.PodSecurityContext{},
		SchedulerName:                 corev1.DefaultSchedulerName,
	}

	if nodeSet.PriorityClass != "" {
		spec.PriorityClassName = nodeSet.PriorityClass
	}

	return kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeSet.StatefulSet.Name,
			Namespace: nodeSet.ParentalCluster.Namespace,
			Labels:    labels,
		},
		Spec: kruisev1b1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         nodeSet.ServiceUmbrella.Name,
			Replicas:            replicas,
			ReserveOrdinals:     reserveOrdinals,
			UpdateStrategy: kruisev1b1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &kruisev1b1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable:  &nodeSet.StatefulSet.MaxUnavailable,
					PodUpdatePolicy: kruisev1b1.InPlaceIfPossiblePodUpdateStrategyType,
					Partition:       ptr.To(int32(0)),
					MinReadySeconds: ptr.To(int32(0)),
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			VolumeClaimTemplates: common.RenderVolumeClaimTemplates(
				consts.ComponentTypeNodeSet,
				nodeSet.ParentalCluster.Namespace,
				nodeSet.ParentalCluster.Name,
				pvcTemplateSpecs,
			),
			VolumeClaimUpdateStrategy: kruisev1b1.VolumeClaimUpdateStrategy{
				Type: kruisev1b1.OnPodRollingUpdateVolumeClaimUpdateStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: renderNodeSetAnnotations(nodeSet),
				},
				Spec: spec,
			},
			PersistentVolumeClaimRetentionPolicy: &kruisev1b1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  kruisev1b1.RetainPersistentVolumeClaimRetentionPolicyType,
			},
		},
	}, nil
}

func renderNodeSetAnnotations(nodeSet *values.SlurmNodeSet) map[string]string {
	annotations := common.RenderDefaultContainerAnnotation(consts.ContainerNameSlurmd)
	maps.Copy(annotations, nodeSet.Annotations)
	return annotations
}

// calculateReplicasAndReserveOrdinals calculates the replicas and reserveOrdinals for ephemeral nodes.
// For activeNodes = [0, 3, 5, 7, 12]:
//   - replicas = 5 (number of active nodes)
//   - reserveOrdinals = [1, 2, 4, 6, 8, 9, 10, 11] (all ordinals from 0 to maxOrdinal that are NOT in activeNodes)
//
// This way, OpenKruise will create pods at ordinals 0, 3, 5, 7, 12 only.
func calculateReplicasAndReserveOrdinals(activeNodes []int32) (*int32, []intstr.IntOrString) {
	if len(activeNodes) == 0 {
		return ptr.To(int32(0)), nil
	}

	// Sort activeNodes to find the max ordinal
	sortedActive := make([]int32, len(activeNodes))
	copy(sortedActive, activeNodes)
	sort.Slice(sortedActive, func(i, j int) bool { return sortedActive[i] < sortedActive[j] })

	maxOrdinal := sortedActive[len(sortedActive)-1]

	// Create a set of active ordinals for O(1) lookup
	activeSet := make(map[int32]bool, len(activeNodes))
	for _, ordinal := range activeNodes {
		activeSet[ordinal] = true
	}

	// Calculate reserveOrdinals: all ordinals from 0 to maxOrdinal that are NOT in activeNodes
	var reserveOrdinals []intstr.IntOrString
	for i := int32(0); i <= maxOrdinal; i++ {
		if !activeSet[i] {
			reserveOrdinals = append(reserveOrdinals, intstr.FromInt32(i))
		}
	}

	// Replicas = number of active nodes
	replicas := int32(len(activeNodes))

	return &replicas, reserveOrdinals
}

// isEphemeralNodesEnabled checks if ephemeral nodes mode is enabled for the NodeSet
func isEphemeralNodesEnabled(nodeSet *values.SlurmNodeSet) bool {
	return nodeSet.EphemeralNodes != nil && *nodeSet.EphemeralNodes
}
