package sconfigcontroller

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

// These should match UID and GID in sconfigcontroller image
const defaultUid int64 = 1001
const defaultGid int64 = 1001

func renderInitContainerSConfigController(
	jailConfigPath string,
	runAsUid *int64,
	runAsGid *int64,
) corev1.Container {
	// Create a copy of the container's limits and add non-CPU resources from Requests

	var uid int64
	if runAsUid != nil {
		uid = *runAsUid
	} else {
		uid = defaultUid
	}

	var gid int64
	if runAsGid != nil {
		gid = *runAsGid
	} else {
		gid = defaultGid
	}

	// restartPolicy := corev1.ContainerRestartPolicyAlways
	return corev1.Container{
		Name:            "init-dir",
		Image:           consts.InitContainerImageSconfigController,
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: ptr.To(int64(0)),
		},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountJail(),
		},
		Env: []corev1.EnvVar{
			{
				Name:  "JAIL_CONFIG_PATH",
				Value: jailConfigPath,
			},
			{
				Name:  "JAIL_UID",
				Value: strconv.FormatInt(uid, 10),
			},
			{
				Name:  "JAIL_GID",
				Value: strconv.FormatInt(gid, 10),
			},
		},
		Command: []string{"/bin/sh", "-c"}, // Use bash to execute the script
		Args: []string{
			// Quotes around variables are load-bearing, so shell would treat them as single string each
			"mkdir -p \"${JAIL_CONFIG_PATH}\" && chown \"${JAIL_UID}:${JAIL_GID}\" \"${JAIL_CONFIG_PATH}\" && chmod 755 \"${JAIL_CONFIG_PATH}\"",
		},
	}
}
