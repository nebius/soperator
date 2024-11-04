package worker

import (
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
	apparmorprofileapi "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"
)

const appArmorPolicyTemplate = `#include <tunables/global>
profile %s flags=(attach_disconnected) {
    #include <abstractions/base>
    # Allow all permissions
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
    # Sensitive files permissions
    /etc/shadow rwl,
    /etc/gshadow rwl,

    # Allow main entrypoint process (PID 1) with exact executable
    /opt/bin/slurm/slurmd_entrypoint.sh ixr,
    # General permissions
    /** rixw,
    # Slurm configuration files
    /etc/slurm/cgroup.conf rw,
    /mnt/slurm-configs/** rw,
    # Library permissions for NVIDIA
    /mnt/jail/usr/lib/**/libnvidia-* r,
    deny /mnt/jail/usr/lib/**/libnvidia-* w,
    /usr/lib/**/libnvidia-* r,
    deny /usr/lib/**/libnvidia-* w,
}`

func RenderAppArmorProfile(cluster *values.SlurmCluster) *apparmorprofileapi.AppArmorProfile {
	profileName := naming.BuildAppArmorProfileWorkerName(cluster.Name)
	return common.RenderAppArmorProfile(
		cluster.Name, cluster.Namespace, profileName, appArmorPolicyTemplate,
	)
}
