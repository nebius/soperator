package common

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"

	apparmor "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"
)

func RenderAppArmorProfile(cluster *values.SlurmCluster) *apparmor.AppArmorProfile {
	if !cluster.NodeLogin.UseDefaultAppArmorProfile || !cluster.NodeWorker.UseDefaultAppArmorProfile {
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


  # remove [^m], when bump slurm 24.05.5 or higher
  
  deny /mnt/jail/usr/lib/x86_64-linux-gnu/libnvidia-[^m]* w,
  deny /mnt/jail/usr/lib/x86_64-linux-gnu/libcuda.so* w,
  deny /mnt/jail/usr/lib/x86_64-linux-gnu/libcudadebugger.so* w,

  deny /usr/lib/x86_64-linux-gnu/libnvidia-[^m]* w,
  deny /usr/lib/x86_64-linux-gnu/libcuda.so* w,
  deny /usr/lib/x86_64-linux-gnu/libcudadebugger.so* w,

  deny /lib/x86_64-linux-gnu/libnvidia-[^m]* w,
  deny /lib/x86_64-linux-gnu/libcuda.so* w,
  deny /lib/x86_64-linux-gnu/libcudadebugger.so* w,

  deny /usr/local/lib/x86_64-linux-gnu/libnvidia-[^m]* w,
  deny /usr/local/lib/x86_64-linux-gnu/libcuda.so* w,
  deny /usr/local/lib/x86_64-linux-gnu/libcudadebugger.so* w,

  deny /usr/local/nvidia/lib/x86_64-linux-gnu/libnvidia-[^m]* w,
  deny /usr/local/nvidia/lib/x86_64-linux-gnu/libcuda.so* w,
  deny /usr/local/nvidia/lib/x86_64-linux-gnu/libcudadebugger.so* w,

  deny /usr/local/nvidia/lib64/libnvidia-[^m]* w,
  deny /usr/local/nvidia/lib64/libcuda.so* w,
  deny /usr/local/nvidia/lib64/libcudadebugger.so* w,

  deny /usr/bin/nvidia-smi w,
  deny /usr/bin/nvidia-debugdump w,
  deny /usr/bin/nvidia-persistenced w,
  deny /usr/bin/nv-fabricmanager w,
  deny /usr/bin/nvidia-cuda-mps-control w,
  deny /usr/bin/nvidia-cuda-mps-server w,


  deny /lib/firmware/nvidia/**/gsp_*.bin w,
}`, naming.BuildAppArmorProfileName(clusterName, namespace))
}
