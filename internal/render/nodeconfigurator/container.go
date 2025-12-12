package nodeconfigurator

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

// renderContainers renders the containers for the DaemonSet
func renderContainers(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) []corev1.Container {
	if nodeConfigurator.Rebooter.Enabled {
		return []corev1.Container{renderContainerRebooter(nodeConfigurator.Rebooter)}
	}
	if nodeConfigurator.CustomContainer.Enabled {
		return []corev1.Container{renderCustomContainer(nodeConfigurator.CustomContainer)}
	}
	return []corev1.Container{}
}

// renderContainerRebooter renders [corev1.Container] for reconciliation of node rebooter
func renderContainerRebooter(rebooter slurmv1alpha1.Rebooter) corev1.Container {
	rebooter.Env = append(
		rebooter.Env,
		corev1.EnvVar{
			Name:  consts.RebooterMethodEnv,
			Value: rebooter.EvictionMethod,
		}, corev1.EnvVar{
			Name: consts.RebooterNodeNameEnv,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
	)

	return corev1.Container{
		Name:            consts.ContainerNameRebooter,
		Image:           rebooter.Image.GetURI(),
		ImagePullPolicy: rebooter.Image.PullPolicy,
		SecurityContext: createSecurityContext(true, 0, 0, true, true, nil),
		Command: []string{
			"/usr/bin/rebooter",
		},
		Args: []string{
			"-log-level=" + rebooter.LogLevel,
			"-log-format=" + rebooter.LogFormat,
		},
		Resources: createResourceRequirements(
			*rebooter.Resources.Limits.Memory(),
			*rebooter.Resources.Requests.Cpu(),
			*rebooter.Resources.Requests.Memory(),
		),
		Env:            rebooter.Env,
		ReadinessProbe: rebooter.ReadinessProbe,
		LivenessProbe:  rebooter.LivenessProbe,
	}
}

func renderCustomContainer(customContainer slurmv1alpha1.CustomContainer) corev1.Container {
	if customContainer.ContainerConfig.Name == "" {
		customContainer.ContainerConfig.Name = consts.ContainerNameCustom
	}

	return corev1.Container{
		Name:            customContainer.ContainerConfig.Name,
		Image:           customContainer.Image.GetURI(),
		SecurityContext: createSecurityContext(false, 65534, 65534, true, false, []corev1.Capability{"ALL"}),
		Resources: createResourceRequirements(
			*customContainer.Resources.Limits.Memory(),
			*customContainer.Resources.Requests.Cpu(),
			*customContainer.Resources.Requests.Memory(),
		),
		Command:        customContainer.Command,
		Env:            customContainer.Env,
		ReadinessProbe: customContainer.ReadinessProbe,
		LivenessProbe:  customContainer.LivenessProbe,
	}
}

// Common function to create a SecurityContext
func createSecurityContext(privileged bool, runAsUser, runAsGroup int64, readOnlyRootFilesystem, allowPrivilegeEscalation bool, dropCapabilities []corev1.Capability) *corev1.SecurityContext {
	if privileged {
		// Cannot set `allowPrivilegeEscalation` to false and `privileged` to true"
		allowPrivilegeEscalation = true
	}
	return &corev1.SecurityContext{
		Privileged:               ptr.To(privileged),
		RunAsUser:                ptr.To(runAsUser),
		RunAsGroup:               ptr.To(runAsGroup),
		RunAsNonRoot:             ptr.To(!privileged),
		ReadOnlyRootFilesystem:   ptr.To(readOnlyRootFilesystem),
		AllowPrivilegeEscalation: ptr.To(allowPrivilegeEscalation),
		Capabilities: &corev1.Capabilities{
			Drop: dropCapabilities,
		},
	}
}

// Common function to create ResourceRequirements
func createResourceRequirements(limitsMemory, requestsCPU, requestsMemory resource.Quantity) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: limitsMemory,
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    requestsCPU,
			corev1.ResourceMemory: requestsMemory,
		},
	}
}
