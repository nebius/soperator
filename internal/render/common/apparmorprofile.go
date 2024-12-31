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

  # set /usr/lib/**/libnvidia-* w, when bump slurm 24.05.5 or higher
  deny /usr/lib/**/libnvidia-[^m]* w,
  deny /mnt/jail/usr/lib/**/libnvidia-[^m]* w,

  deny /usr/lib/**/libcuda.so* w,
  deny /usr/lib/**/libcudadebugger.so* w,
  deny /usr/lib/**/libcudadebugger.so* w,
  deny /usr/bin/nvidia-smir w,
  deny /usr/bin/nvidia-debugdumpr w,
  deny /usr/bin/nvidia-persistencedr w,
  deny /usr/bin/nv-fabricmanagerr w,
  deny /usr/bin/nvidia-cuda-mps-controlr w,
  deny /usr/bin/nvidia-cuda-mps-server w,
  deny /lib/firmware/nvidia/**/gsp_*.bin w,
}`, naming.BuildAppArmorProfileName(clusterName, namespace))
}
