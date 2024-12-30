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
  deny /mnt/jail/usr/lib/**/libnvidia--[^m]* w,
}`, naming.BuildAppArmorProfileName(clusterName, namespace))
}
