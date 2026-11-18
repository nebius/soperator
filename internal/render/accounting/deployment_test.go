package accounting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/accounting"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderDeployment(t *testing.T) {

	deployment, err := accounting.RenderDeployment(defaultNamespace, defaultNameCluster, acc, &values.SlurmController{}, defaultNodeFilter, defaultVolumeSources)
	assert.NoError(t, err)

	assert.Equal(t, acc.Deployment.Name, deployment.Name)
	assert.Equal(t, defaultNamespace, deployment.Namespace)
	assert.Equal(t, common.RenderLabels(consts.ComponentTypeAccounting, defaultNameCluster), deployment.Labels)

	assert.Equal(t, &acc.Deployment.Replicas, deployment.Spec.Replicas)
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)
	assert.Equal(t, common.RenderMatchLabels(consts.ComponentTypeAccounting, defaultNameCluster), deployment.Spec.Selector.MatchLabels)

}

func TestRenderDeploymentIdentityResolution(t *testing.T) {
	var tests = []struct {
		name       string
		sssd       bool
		secretName string
		ldapCA     string
	}{
		{
			name:       "Local users without SSSD",
			secretName: "unused-sssd-secret",
			ldapCA:     "unused-ldap-ca",
		},
		{
			name:       "SSSD with controller default config",
			sssd:       true,
			secretName: naming.BuildSecretSSSDConfName(defaultNameCluster),
		},
		{
			name:       "SSSD with custom config",
			sssd:       true,
			secretName: "custom-sssd-secret",
		},
		{
			name:       "SSSD with custom config and LDAP CA",
			sssd:       true,
			secretName: "custom-sssd-secret",
			ldapCA:     "ldap-ca",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &values.SlurmController{
				SSSDConfSecretName:      tt.secretName,
				SSSDLdapCAConfigMapName: tt.ldapCA,
			}
			if tt.sssd {
				controller.ContainerSSSD = &values.Container{
					Name: consts.ContainerNameSSSD,
					NodeContainer: slurmv1.NodeContainer{
						Image:           "custom-sssd:latest",
						ImagePullPolicy: corev1.PullAlways,
						Command:         []string{"/usr/sbin/sssd"},
						Args:            []string{"--interactive", "--logger=stderr"},
						CustomEnv:       []corev1.EnvVar{{Name: "SSSD_TEST", Value: "inherited"}},
						Resources: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
							Exec: &corev1.ExecAction{Command: []string{"test", "-S", "/var/lib/sss/pipes/nss"}},
						}},
					},
				}
			}
			testAcc := *acc
			testAcc.ContainerAccounting.CustomEnv = []corev1.EnvVar{{Name: "ACCOUNTING_TEST", Value: "preserved"}}
			testAcc.CustomInitContainers = []corev1.Container{{Name: "custom-init", Image: "custom-init:latest"}}
			testAcc.ExternalDB.TLS.ServerCASecretRef = "database-ca"

			deployment, err := accounting.RenderDeployment(
				defaultNamespace, defaultNameCluster, &testAcc, controller, defaultNodeFilter, defaultVolumeSources,
			)
			require.NoError(t, err)
			pod := deployment.Spec.Template.Spec
			require.Len(t, pod.Containers, 1)
			slurmdbd := pod.Containers[0]
			assert.Contains(t, slurmdbd.VolumeMounts, corev1.VolumeMount{
				Name: "jail", MountPath: "/mnt/jail", ReadOnly: true,
			})
			assert.Contains(t, slurmdbd.VolumeMounts, accounting.RenderVolumeMountSlurmdbdSSLCACertificate())
			assert.Contains(t, slurmdbd.Env, corev1.EnvVar{Name: "ACCOUNTING_TEST", Value: "preserved"})
			assert.Equal(t, testAcc.CustomInitContainers[0], pod.InitContainers[0])

			volumes := make(map[string]corev1.Volume, len(pod.Volumes))
			for _, volume := range pod.Volumes {
				volumes[volume.Name] = volume
			}
			var sssd *corev1.Container
			for i := range pod.InitContainers {
				if pod.InitContainers[i].Name == consts.ContainerNameSSSD {
					sssd = &pod.InitContainers[i]
				}
			}
			if !tt.sssd {
				assert.Nil(t, sssd)
				assert.NotContains(t, volumes, consts.VolumeNameSSSDSocket)
				assert.NotContains(t, volumes, consts.VolumeNameSSSDConf)
				assert.NotContains(t, volumes, consts.VolumeNameSSSDLdapCA)
				assert.Equal(t, testAcc.ContainerAccounting.CustomEnv, slurmdbd.Env)
				for _, mount := range slurmdbd.VolumeMounts {
					assert.NotEqual(t, consts.VolumeNameSSSDSocket, mount.Name)
					assert.NotEqual(t, consts.VolumeNameSSSDConf, mount.Name)
				}
				return
			}

			require.NotNil(t, sssd)
			assert.Equal(t, controller.ContainerSSSD.Image, sssd.Image)
			assert.Equal(t, controller.ContainerSSSD.ImagePullPolicy, sssd.ImagePullPolicy)
			assert.Equal(t, controller.ContainerSSSD.Command, sssd.Command)
			assert.Equal(t, controller.ContainerSSSD.Args, sssd.Args)
			assert.Equal(t, controller.ContainerSSSD.CustomEnv, sssd.Env)
			assert.Equal(t, controller.ContainerSSSD.Resources, sssd.Resources.Requests)
			assert.Equal(t, controller.ContainerSSSD.ReadinessProbe, sssd.ReadinessProbe)
			assert.Equal(t, ptr.To(corev1.ContainerRestartPolicyAlways), sssd.RestartPolicy)
			assert.Contains(t, slurmdbd.Env, corev1.EnvVar{Name: "SOPERATOR_SSSD_ENABLED", Value: "true"})
			require.NotNil(t, volumes[consts.VolumeNameSSSDSocket].EmptyDir)
			require.NotNil(t, volumes[consts.VolumeNameSSSDConf].Secret)
			assert.Equal(t, tt.secretName, volumes[consts.VolumeNameSSSDConf].Secret.SecretName)
			assert.Equal(t, ptr.To(int32(0600)), volumes[consts.VolumeNameSSSDConf].Secret.DefaultMode)
			for _, container := range []corev1.Container{slurmdbd, *sssd} {
				assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{
					Name: consts.VolumeNameSSSDSocket, MountPath: "/var/lib/sss/pipes",
				})
				assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{
					Name: consts.VolumeNameSSSDConf, MountPath: "/etc/sssd", ReadOnly: true,
				})
			}
			if tt.ldapCA == "" {
				assert.NotContains(t, volumes, consts.VolumeNameSSSDLdapCA)
				for _, mount := range sssd.VolumeMounts {
					assert.NotEqual(t, consts.VolumeNameSSSDLdapCA, mount.Name)
				}
			} else {
				require.NotNil(t, volumes[consts.VolumeNameSSSDLdapCA].ConfigMap)
				assert.Equal(t, tt.ldapCA, volumes[consts.VolumeNameSSSDLdapCA].ConfigMap.Name)
				assert.Contains(t, sssd.VolumeMounts, corev1.VolumeMount{
					Name: consts.VolumeNameSSSDLdapCA, MountPath: "/mnt/ldapCA", ReadOnly: true,
				})
			}
		})
	}
}
