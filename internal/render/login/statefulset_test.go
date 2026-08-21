package login

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderStatefulSet_PriorityClass(t *testing.T) {
	tests := []struct {
		name          string
		priorityClass string
		expectedClass string
	}{
		{
			name:          "empty priority class",
			priorityClass: "",
			expectedClass: "",
		},
		{
			name:          "custom priority class",
			priorityClass: "high-priority",
			expectedClass: "high-priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test data
			namespace := "test-namespace"
			clusterName := "test-cluster"
			nodeFilters := []slurmv1.K8sNodeFilter{
				{
					Name: "test-filter",
				},
			}
			secrets := &slurmv1.Secrets{}
			volumeSources := []slurmv1.VolumeSource{
				{
					Name: "test-volume",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{},
					},
				},
			}

			// Complete login configuration
			login := &values.SlurmLogin{
				SlurmNode: slurmv1.SlurmNode{
					K8sNodeFilterName: "test-filter",
					PriorityClass:     tt.priorityClass,
				},
				ContainerSshd: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-sshd-image",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Port:            22,
						Resources: corev1.ResourceList{
							corev1.ResourceMemory:           resource.MustParse("1Gi"),
							corev1.ResourceCPU:              resource.MustParse("100m"),
							corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
						},
					},
				},
				ContainerMunge: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-munge-image",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Resources: corev1.ResourceList{
							corev1.ResourceMemory:           resource.MustParse("1Gi"),
							corev1.ResourceCPU:              resource.MustParse("100m"),
							corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
						},
					},
				},
				VolumeJail: slurmv1.NodeVolume{
					VolumeSourceName: &[]string{"test-volume"}[0],
				},
				StatefulSet: values.StatefulSet{
					Name:     "test-login",
					Replicas: 1,
				},
				HeadlessService: values.Service{
					Name: "test-headless",
				},
				SSHDConfigMapName:    "test-sshd-config",
				CustomInitContainers: []corev1.Container{},
				JailSubMounts:        []slurmv1.NodeVolumeMount{},
				CustomVolumeMounts:   []slurmv1.NodeVolumeMount{},
			}

			// Render StatefulSet
			result, err := RenderStatefulSet(
				namespace,
				clusterName,
				true,
				nodeFilters,
				secrets,
				volumeSources,
				login,
			)

			if err != nil {
				t.Fatalf("RenderStatefulSet() error = %v", err)
			}

			// Check PriorityClassName
			if result.Spec.Template.Spec.PriorityClassName != tt.expectedClass {
				t.Errorf("PriorityClassName = %v, want %v", result.Spec.Template.Spec.PriorityClassName, tt.expectedClass)
			}
		})
	}
}

func TestRenderStatefulSet_Docker(t *testing.T) {
	newLogin := func(dockerEnabled, withStorage bool) *values.SlurmLogin {
		login := &values.SlurmLogin{
			SlurmNode: slurmv1.SlurmNode{K8sNodeFilterName: "test-filter"},
			ContainerSshd: values.Container{NodeContainer: slurmv1.NodeContainer{
				Image: "test-sshd-image",
				Resources: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}},
			ContainerMunge: values.Container{NodeContainer: slurmv1.NodeContainer{Image: "test-munge-image"}},
			VolumeJail:     slurmv1.NodeVolume{VolumeSourceName: ptr.To("test-volume")},
			StatefulSet:    values.StatefulSet{Name: "test-login", Replicas: 1},
			HeadlessService: values.Service{
				Name: "test-headless",
			},
			SSHDConfigMapName: "test-sshd-config",
			DockerEnabled:     dockerEnabled,
		}
		if withStorage {
			login.JailSubMounts = []slurmv1.NodeVolumeMount{
				{
					Name:                    "image-storage",
					MountPath:               consts.DockerImageStorageMountPath,
					VolumeClaimTemplateSpec: &corev1.PersistentVolumeClaimSpec{},
				},
			}
		}
		return login
	}

	render := func(login *values.SlurmLogin) (corev1.PodSpec, error) {
		result, err := RenderStatefulSet(
			"test-namespace",
			"test-cluster",
			false,
			[]slurmv1.K8sNodeFilter{{Name: "test-filter"}},
			&slurmv1.Secrets{},
			[]slurmv1.VolumeSource{{
				Name:         "test-volume",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			}},
			login,
		)
		return result.Spec.Template.Spec, err
	}

	t.Run("disabled preserves the single-container pod", func(t *testing.T) {
		podSpec, err := render(newLogin(false, false))
		assert.NoError(t, err)
		assert.Len(t, podSpec.Containers, 1)
		assert.Empty(t, envValue(podSpec.Containers[0].Env, consts.EnvDockerEnabled))
		for _, volume := range podSpec.Volumes {
			assert.NotEqual(t, consts.VolumeNameRuntime, volume.Name)
		}
	})

	t.Run("enabled requires image storage", func(t *testing.T) {
		_, err := render(newLogin(true, false))
		assert.ErrorContains(t, err, consts.DockerImageStorageMountPath)
	})

	t.Run("enabled rejects read-only image storage", func(t *testing.T) {
		login := newLogin(true, true)
		login.JailSubMounts[0].ReadOnly = true

		_, err := render(login)
		assert.ErrorContains(t, err, "must be writable")
	})

	t.Run("enabled renders proxy and shared runtime", func(t *testing.T) {
		podSpec, err := render(newLogin(true, true))
		assert.NoError(t, err)
		assert.Len(t, podSpec.Containers, 2)

		var sshd, proxy *corev1.Container
		for i := range podSpec.Containers {
			container := &podSpec.Containers[i]
			switch container.Name {
			case consts.ContainerNameSshd:
				sshd = container
			case consts.ContainerNameDockerProxy:
				proxy = container
			}
		}
		if assert.NotNil(t, sshd) {
			assert.Equal(t, "true", envValue(sshd.Env, consts.EnvDockerEnabled))
			assert.True(t, hasVolumeMount(sshd.VolumeMounts, consts.VolumeNameRuntime, consts.VolumeMountPathRuntime))
			assert.True(t, hasVolumeMount(sshd.VolumeMounts, "image-storage", consts.DockerImageStorageMountPath))
			assert.True(t, hasVolumeMount(sshd.VolumeMounts, "image-storage", "/mnt/jail.upper/mnt/image-storage"))
		}
		if assert.NotNil(t, proxy) {
			assert.Equal(t, "test-sshd-image", proxy.Image)
			assert.Equal(t, []string{"/opt/bin/slurm/login-docker-proxy"}, proxy.Command)
			assert.True(t, hasVolumeMount(proxy.VolumeMounts, consts.VolumeNameRuntime, consts.VolumeMountPathRuntime))
		}

		var hasRuntime bool
		for _, volume := range podSpec.Volumes {
			if volume.Name == consts.VolumeNameRuntime && volume.EmptyDir != nil {
				hasRuntime = true
			}
		}
		assert.True(t, hasRuntime)
	})
}

func envValue(env []corev1.EnvVar, name string) string {
	for _, variable := range env {
		if variable.Name == name {
			return variable.Value
		}
	}
	return ""
}

func hasVolumeMount(mounts []corev1.VolumeMount, name, mountPath string) bool {
	for _, mount := range mounts {
		if mount.Name == name && mount.MountPath == mountPath {
			return true
		}
	}
	return false
}
