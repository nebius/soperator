package accounting

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/utils/stringutils"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerAccounting renders [corev1.Container] for slurmctld
func renderContainerAccounting(container values.Container, additionalVolumeMounts []corev1.VolumeMount) corev1.Container {
	if container.Port == 0 {
		container.Port = consts.DefaultAccountingPort
	}
	container.NodeContainer.Resources.Storage()

	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountSlurmConfigs(),
		common.RenderVolumeMountMungeSocket(),
		common.RenderVolumeMountRESTJWTKey(),
		RenderVolumeMountSlurmdbdSpool(),
	}
	volumeMounts = append(volumeMounts, additionalVolumeMounts...)

	// Create a copy of the container's limits and add non-CPU resources from Requests
	limits := common.CopyNonCPUResources(container.Resources)
	return corev1.Container{
		Name:            consts.ContainerNameAccounting,
		Image:           container.Image,
		Command:         container.Command,
		Args:            container.Args,
		ImagePullPolicy: container.ImagePullPolicy,
		Ports: []corev1.ContainerPort{{
			Name:          container.Name,
			ContainerPort: container.Port,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: volumeMounts,
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(container.Port),
				},
			},
			FailureThreshold:    3,
			InitialDelaySeconds: 1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			TimeoutSeconds:      1,
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
			AppArmorProfile: common.ParseAppArmorProfile(container.AppArmorProfile),
		},
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: container.Resources,
		},
	}
}

// renderContainerDbwaiter renders accounting DB waiter init container.
func renderContainerDbwaiter(clusterName string, accounting *values.SlurmAccounting) corev1.Container {
	secretReference := corev1.LocalObjectReference{
		Name: naming.BuildSecretSlurmdbdConfigsName(clusterName),
	}

	var env []corev1.EnvVar
	for _, envToKey := range []struct {
		env string
		key string
	}{{
		env: consts.AccountingStorageHostEnv, key: consts.SecretSlurmdbdConfigStorageHost,
	}, {
		env: consts.AccountingStoragePortEnv, key: consts.SecretSlurmdbdConfigStoragePort,
	}, {
		env: consts.AccountingStorageUserEnv, key: consts.SecretSlurmdbdConfigStorageUser,
	}, {
		env: consts.AccountingStoragePassEnv, key: consts.SecretSlurmdbdConfigStoragePass,
	}} {
		env = append(env, corev1.EnvVar{
			Name: envToKey.env,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: secretReference,
					Key:                  envToKey.key,
				},
			},
		})
	}

	return corev1.Container{
		Name: consts.ContainerNameWaitForDatabase,
		Image: utils.Ternary(
			len(accounting.MariaDb.Image) > 0,
			accounting.MariaDb.Image,
			"docker-registry1.mariadb.com/library/mariadb:12.1.2",
		),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             env,
		Command: []string{
			"/bin/sh", "-c",
			// language=bash
			stringutils.Dedent(`
			set -eu
			
			HOST="${STORAGE_HOST:-soperator-acct-db}"
			PORT="${STORAGE_PORT:-3306}"
			USER="${STORAGE_USER:-slurm}"
			PASS="${STORAGE_PASS:-}"
			
			CONNECT_TIMEOUT=3
			TIMEOUT=300
			INTERVAL=5
			
			start_ts="$(date +%s)"
			end_ts=$((start_ts + TIMEOUT))
			
			echo "Waiting for DB at ${HOST}:${PORT} (timeout=${TIMEOUT}s, interval=${INTERVAL}s)..."
			
			while :; do
			  now_ts="$(date +%s)"
			  if [ "${now_ts}" -ge "${end_ts}" ]; then
			    echo "ERROR: timed out waiting for DB at ${HOST}:${PORT}"
			    exit 1
			  fi
			
			  if mariadb \
			    --connect-timeout="${CONNECT_TIMEOUT}" \
			    -h "${HOST}" \
			    -P "${PORT}" \
			    -u "${USER}" \
			    -p"${PASS}" \
			    -e "SELECT 1;" >/dev/null 2>&1
			  then
			    echo "DB is ready."
			    exit 0
			  fi
			
			  echo "DB is not ready at ${HOST}:${PORT}, retrying in ${INTERVAL}s..."
			  sleep "${INTERVAL}"
			done
			`),
		},
	}
}
