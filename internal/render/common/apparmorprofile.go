package common

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"

	apparmor "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"
)

func RenderAppArmorProfile(cluster *values.SlurmCluster) *apparmor.AppArmorProfile {
	if !cluster.UseDefaultAppArmorProfile {
		return nil
	}
	return &apparmor.AppArmorProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildAppArmorProfileName(cluster.Name, cluster.Namespace),
			Namespace: cluster.Namespace,
		},
		Spec: apparmor.AppArmorProfileSpec{
			Policy: generateAppArmorPolicy(cluster.Name, cluster.Namespace),
		},
	}
}

func generateAppArmorPolicy(clusterName, namespace string) string {
	return fmt.Sprintf(`#include <tunables/global>

profile %s flags=(attach_disconnected,mediate_deleted) {
  include <abstractions/base>

  file,
  mount,
  capability,
  network,
  dbus,
  pivot_root,
  remount,
  ptrace,
  signal,
  umount,
  unix,

  /** lrixw,

  deny /usr/lib/x86_64-linux-gnu/libGLES* w,
  deny /usr/lib/x86_64-linux-gnu/libGLX_nvidia* w,
  deny /usr/lib/x86_64-linux-gnu/libnvcuvid* w,
  deny /usr/lib/x86_64-linux-gnu/gbm/nvidia-* w,
  deny /usr/lib/x86_64-linux-gnu/nvidia/wine/_nvngx.dll w,
  deny /usr/lib/x86_64-linux-gnu/nvidia/wine/nvngx.dll w,
  deny /usr/lib/x86_64-linux-gnu/nvidia/xorg/libglxserver_nvidia* w,
  deny /usr/lib/x86_64-linux-gnu/nvidia/xorg/nvidia_drv.so w,
  deny /usr/lib/x86_64-linux-gnu/vdpau/libvdpau_nvidia w,
  deny /usr/lib/x86_64-linux-gnu/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/lib/x86_64-linux-gnu/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/lib/x86_64-linux-gnu/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,

  deny /lib/x86_64-linux-gnu/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/local/lib/x86_64-linux-gnu/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /lib/x86_64-linux-gnu/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,

  deny /usr/local/lib/x86_64-linux-gnu/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/local/lib/x86_64-linux-gnu/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/local/lib/x86_64-linux-gnu/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,

  deny /usr/local/nvidia/lib/x86_64-linux-gnu/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/local/nvidia/lib/x86_64-linux-gnu/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/local/nvidia/lib/x86_64-linux-gnu/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,

  deny /usr/local/nvidia/lib64/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/local/nvidia/lib64/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,
  deny /usr/local/nvidia/lib64/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]* w,

  deny /usr/bin/nvidia-smi w,
  deny /usr/bin/nvidia-debugdump w,
  deny /usr/bin/nvidia-persistenced w,
  deny /usr/bin/nv-fabricmanager w,
  deny /usr/bin/nvidia-cuda-mps-control w,
  deny /usr/bin/nvidia-cuda-mps-server w,
  deny /lib/firmware/nvidia/**/gsp_*.bin w,
}`, naming.BuildAppArmorProfileName(clusterName, namespace))
}

// ParseAppArmorProfile converts AppArmor profile string to corev1.AppArmorProfile
// It supports formats like "unconfined", "localhost/profile-name", or just "profile-name"
func ParseAppArmorProfile(profileStr string) *corev1.AppArmorProfile {
	if profileStr == "" {
		return nil
	}
	if profileStr == "unconfined" {
		return &corev1.AppArmorProfile{
			Type: corev1.AppArmorProfileTypeUnconfined,
		}
	}
	if strings.HasPrefix(profileStr, "localhost/") {
		profileName := strings.TrimPrefix(profileStr, "localhost/")
		return &corev1.AppArmorProfile{
			Type:             corev1.AppArmorProfileTypeLocalhost,
			LocalhostProfile: ptr.To(profileName),
		}
	}
	return &corev1.AppArmorProfile{
		Type:             corev1.AppArmorProfileTypeLocalhost,
		LocalhostProfile: ptr.To(profileStr),
	}
}
