package worker

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/utils/sliceutils"
	"nebius.ai/slurm-operator/internal/utils/stringutils"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderContainerWorkerInit renders init [corev1.Container] for worker nodes.
// It runs a script that waits for the controller to be ready before allowing the main slurmd container to start.
// If topology is enabled, it also resolves the topology this worker registers into and leaves it on
// the runtime volume, so that slurmd starts without waiting for the topology config itself.
func RenderContainerWorkerInit(
	container *values.Container,
	topologyEnabled, gpuEnabled bool,
	waitTimeoutSeconds int32,
	topologyFabric string,
	randomDelaySeconds int32,
) corev1.Container {
	command := []string{
		"python3",
		"/opt/bin/slurm/worker_init.py",
		"wait-controller",
	}
	if topologyEnabled {
		command = append(command, "wait-topology")
	}

	var volumeMounts []corev1.VolumeMount
	if topologyEnabled {
		// The runtime volume carries the resolved topology to slurmd. It is mounted first because
		// the munge socket below sits inside it, and a nested mount must follow its parent.
		volumeMounts = append(volumeMounts,
			renderVolumeMountRuntime(),
			corev1.VolumeMount{
				Name:      consts.VolumeNameTopologyNodeLabels,
				MountPath: consts.VolumeMountPathTopologyNodeLabels,
				ReadOnly:  true,
			},
		)
	}
	volumeMounts = append(volumeMounts,
		common.RenderVolumeMountJail(),
		common.RenderVolumeMountMungeSocket(),
	)

	env := []corev1.EnvVar{
		{
			Name:  "CONTROLLER_MAX_ATTEMPTS",
			Value: "60",
		},
		{
			Name:  "CONTROLLER_POLL_INTERVAL",
			Value: "5",
		},
	}

	if gpuEnabled {
		env = append(env, corev1.EnvVar{
			Name:  "NODESET_GPU_ENABLED",
			Value: "true",
		})
	}

	if topologyEnabled {
		env = append(env, renderNodeSetTopologyEnv(topologyFabric, waitTimeoutSeconds)...)
	}

	if randomDelaySeconds > 0 {
		env = append(env, corev1.EnvVar{
			Name:  "WORKER_INIT_RANDOM_DELAY_SECONDS",
			Value: strconv.Itoa(int(randomDelaySeconds)),
		})
	}

	return corev1.Container{
		Name:                     consts.ContainerNameWorkerInit,
		Image:                    container.Image,
		ImagePullPolicy:          container.ImagePullPolicy,
		Command:                  command,
		VolumeMounts:             volumeMounts,
		Env:                      env,
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

// Wait for the host socket before creating slurmd so the NVIDIA runtime can discover and inject it.
func renderContainerNvidiaPersistencedWaiter(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameWaitForNvidiaPersistenced,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Command:         []string{"/bin/sh", "-ec"},
		Args: []string{
			// language=bash
			fmt.Sprintf(stringutils.Dedent(`
			until [ -S %s/nvidia-persistenced/socket ]; do
			    echo "Waiting for the NVIDIA persistenced socket on the host..."
			    sleep 5
			done
			echo "NVIDIA persistenced socket is ready"
			`), consts.VolumeMountPathHostRun),
		},
		VolumeMounts: []corev1.VolumeMount{
			renderVolumeMountHostRun(),
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

// renderContainerNodeSetSlurmd renders [corev1.Container] for slurmd
func renderContainerNodeSetSlurmd(
	nodeSet *values.SlurmNodeSet,
	topologyEnabled bool,
	cgroupVersion string,
	clusterWithGPU bool,
) (corev1.Container, error) {
	volumeMounts := []corev1.VolumeMount{
		renderVolumeMountRuntime(),
		common.RenderVolumeMountSpool(consts.ComponentTypeWorker, consts.SlurmdName),
		common.RenderVolumeMountJail(),
		common.RenderVolumeMountMungeSocket(),
		common.RenderVolumeMountSecurityLimits(),
		common.RenderVolumeMountSshdKeys(),
		common.RenderVolumeMountSshdRootKeys(),
		common.RenderVolumeMountInMemory(),
		common.RenderVolumeMountTmpDisk(),
		renderVolumeMountBoot(),
		renderVolumeMountHostLogJournal(),
		renderVolumeMountSoperatorOutputs(),
		renderVolumeMountSharedMemory(),
		renderVolumeMountSysctl(),
		renderVolumeMountSupervisordConfigMap(),
		renderVolumeMountSshdConfigs(),
	}
	if nodeSet.ContainerSSSD != nil {
		volumeMounts = append(volumeMounts,
			common.RenderVolumeMountSSSDSocket(),
			common.RenderVolumeMountSSSDConf(),
		)
	}
	if nodeSet.GPU.Enabled {
		volumeMounts = append(volumeMounts, renderVolumeMountNvidia())
		volumeMounts = append(volumeMounts, common.RenderVolumeMountsNvidiaIMEX()...)
	}
	if topologyEnabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      consts.VolumeNameTopologyNodeLabels,
			MountPath: consts.VolumeMountPathTopologyNodeLabels,
			ReadOnly:  true,
		})
	}

	// region Jail Sub-mounts
	volumeMounts = append(volumeMounts,
		common.RenderVolumeMounts(
			sliceutils.MapSlice(nodeSet.JailSubMounts,
				func(subMount slurmv1alpha1.NodeVolumeMount) slurmv1.NodeVolumeMount {
					return slurmv1.NodeVolumeMount{
						Name:      subMount.Name,
						MountPath: subMount.MountPath,
						SubPath:   subMount.SubPath,
						ReadOnly:  subMount.ReadOnly,
					}
				},
			),
			consts.VolumeMountPathJailUpper,
		)...,
	)
	// endregion Jail Sub-mounts

	// region Custom mounts
	volumeMounts = append(volumeMounts,
		common.RenderVolumeMounts(
			sliceutils.MapSlice(nodeSet.CustomVolumeMounts,
				func(mount slurmv1alpha1.NodeVolumeMount) slurmv1.NodeVolumeMount {
					return slurmv1.NodeVolumeMount{
						Name:      mount.Name,
						MountPath: mount.MountPath,
						SubPath:   mount.SubPath,
						ReadOnly:  mount.ReadOnly,
					}
				},
			),
			"",
		)...,
	)
	// endregion Custom mounts

	resources := corev1.ResourceRequirements{
		Limits:   nodeSet.ContainerSlurmd.Resources,
		Requests: nodeSet.ContainerSlurmd.Resources,
	}

	err := check.CheckResourceRequests(resources)
	if err != nil {
		return corev1.Container{}, fmt.Errorf("checking resource requests: %w", err)
	}

	for _, env := range nodeSet.ContainerSlurmd.CustomEnv {
		if env.Name == consts.EnvNodeRealMemoryBytes {
			return corev1.Container{}, fmt.Errorf("environment variable %q is managed by Soperator", consts.EnvNodeRealMemoryBytes)
		}
	}

	realMemoryBytes := common.RenderRealMemorySlurmd(resources) * 1024 * 1024

	appArmorProfile := nodeSet.ContainerSlurmd.AppArmorProfile
	if nodeSet.AppArmorProfileUseDefault {
		appArmorProfile = fmt.Sprintf("%s/%s", "localhost", naming.BuildAppArmorProfileName(nodeSet.ParentalCluster.Name, nodeSet.ParentalCluster.Namespace))
	}
	if appArmorProfile == "" {
		appArmorProfile = consts.AppArmorProfileUnconfined
	}

	return corev1.Container{
		Name:            consts.ContainerNameSlurmd,
		Image:           nodeSet.ContainerSlurmd.Image,
		ImagePullPolicy: nodeSet.ContainerSlurmd.ImagePullPolicy,
		Command:         nodeSet.ContainerSlurmd.Command,
		Args:            nodeSet.ContainerSlurmd.Args,
		Env: append(
			append(
				renderNodeSetSlurmdEnv(
					cgroupVersion,
					clusterWithGPU,
					nodeSet.GPU.Enabled,
					nodeSet.GPU.Nvidia.GDRCopyEnabled,
					nodeSet.DockerEnabled,
					nodeSet.NodeExtra,
					realMemoryBytes,
				),
				renderSlurmdTopologyEnv(topologyEnabled)...,
			),
			nodeSet.ContainerSlurmd.CustomEnv...,
		),
		Ports: []corev1.ContainerPort{{
			Name:          nodeSet.ContainerSlurmd.Name,
			ContainerPort: nodeSet.ContainerSlurmd.Port,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: volumeMounts,
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeUnconfined,
			},
			ProcMount: func() *corev1.ProcMountType {
				if nodeSet.ContainerSlurmd.ProcMount == "" {
					return nil
				}
				v := nodeSet.ContainerSlurmd.ProcMount
				return &v
			}(),
			AppArmorProfile: common.ParseAppArmorProfile(appArmorProfile),
		},
		Resources:                resources,
		LivenessProbe:            nodeSet.ContainerSlurmd.LivenessProbe,
		ReadinessProbe:           nodeSet.ContainerSlurmd.ReadinessProbe,
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}, nil
}

func renderContainerNodeSetDockerProxy(nodeSet *values.SlurmNodeSet) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameDockerProxy,
		Image:           nodeSet.ContainerSlurmd.Image,
		ImagePullPolicy: nodeSet.ContainerSlurmd.ImagePullPolicy,
		Command:         []string{"/opt/bin/slurm/docker_proxy_nginx_entrypoint.sh"},
		VolumeMounts: []corev1.VolumeMount{
			renderVolumeMountRuntime(),
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

func renderVolumeMountSupervisordConfigMap() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSupervisordConfigMap,
		MountPath: consts.VolumeMountPathSupervisordConfig,
		ReadOnly:  true,
	}
}

func renderVolumeMountRuntime() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameRuntime,
		MountPath: consts.VolumeMountPathRuntime,
	}
}

// renderSlurmdTopologyEnv points slurmd at the topology worker-init resolved for it.
func renderSlurmdTopologyEnv(topologyEnabled bool) []corev1.EnvVar {
	if !topologyEnabled {
		return nil
	}

	return []corev1.EnvVar{
		{
			Name:  "SLURM_TOPOLOGY_ENABLED",
			Value: "true",
		},
		{
			Name:  "SLURMD_TOPOLOGY_PATH",
			Value: consts.SlurmdTopologyPath,
		},
	}
}

// renderNodeSetTopologyEnv renders the environment the topology resolver reads in worker-init.
func renderNodeSetTopologyEnv(topologyFabric string, waitTimeoutSeconds int32) []corev1.EnvVar {
	fabric := topologyFabric
	if fabric == "" {
		fabric = consts.SlurmTopologyDefaultFabric
	}

	return []corev1.EnvVar{
		{
			Name: "K8S_NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: corev1.SchemeGroupVersion.Version,
					FieldPath:  "spec.nodeName",
				},
			},
		},
		{
			Name:  "TOPOLOGY_CONFIGMAP_PATH",
			Value: consts.VolumeMountPathTopologyNodeLabels,
		},
		{
			Name:  "TOPOLOGY_WAIT_TIMEOUT",
			Value: strconv.Itoa(int(waitTimeoutSeconds)),
		},
		{
			Name:  "TOPOLOGY_POLL_INTERVAL",
			Value: "5",
		},
		{
			Name:  "SLURM_TOPOLOGY_FABRIC",
			Value: fabric,
		},
		{
			Name:  "SLURMD_TOPOLOGY_PATH",
			Value: consts.SlurmdTopologyPath,
		},
	}
}

func renderNodeSetSlurmdEnv(
	cgroupVersion string,
	clusterWithGPU bool,
	nodeSetGPUEnabled bool,
	enableGDRCopy bool,
	dockerEnabled bool,
	slurmNodeExtra string,
	realMemoryBytes int64,
) []corev1.EnvVar {
	envVar := []corev1.EnvVar{
		{
			Name: "INSTANCE_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: corev1.SchemeGroupVersion.Version,
					FieldPath:  "spec.nodeName",
				},
			},
		},
		{
			Name:  "SLURM_CLUSTER_WITH_GPU",
			Value: strconv.FormatBool(clusterWithGPU),
		},
		{
			Name:  "NODESET_GPU_ENABLED",
			Value: strconv.FormatBool(nodeSetGPUEnabled),
		},
		{
			Name:  consts.EnvDockerEnabled,
			Value: strconv.FormatBool(dockerEnabled),
		},
		{
			Name:  consts.EnvNodeRealMemoryBytes,
			Value: strconv.FormatInt(realMemoryBytes, 10),
		},
	}

	if len(slurmNodeExtra) > 0 {
		envVar = append(envVar, corev1.EnvVar{
			Name:  "SLURM_NODE_EXTRA",
			Value: slurmNodeExtra,
		})
	}
	if cgroupVersion == consts.CGroupV2 {
		envVar = append(envVar, corev1.EnvVar{
			Name:  consts.EnvCGroupV2,
			Value: "true",
		})
	}
	if enableGDRCopy {
		envVar = append(envVar, corev1.EnvVar{
			Name:  consts.EnvNvidiaGDRCopy,
			Value: "enabled",
		})
	}

	return envVar
}
