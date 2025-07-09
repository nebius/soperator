package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"

	apparmorprofilev1alpha1 "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"
	profilebasev1alpha1 "sigs.k8s.io/security-profiles-operator/api/profilebase/v1alpha1"
)

func RenderAppArmorProfile(cluster *values.SlurmCluster) *apparmorprofilev1alpha1.AppArmorProfile {
	isDisabled := cluster.NodeLogin.UseDefaultAppArmorProfile || cluster.NodeWorker.UseDefaultAppArmorProfile
	return &apparmorprofilev1alpha1.AppArmorProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildAppArmorProfileName(cluster.Name, cluster.Namespace),
			Namespace: cluster.Namespace,
		},
		Spec: apparmorprofilev1alpha1.AppArmorProfileSpec{
			Abstract: renderAppArmorAbstract(),
			SpecBase: profilebasev1alpha1.SpecBase{
				Disabled: isDisabled,
			},
		},
	}
}

func renderAppArmorAbstract() apparmorprofilev1alpha1.AppArmorAbstract {
	// Start with minimal implementation to see what fields are actually available
	return apparmorprofilev1alpha1.AppArmorAbstract{
		Executable: renderExecutableRules(),
		Filesystem: renderFileSystemRules(),
		Network:    renderNetworkRules(),
		Capability: renderCapabilityRules(),
	}
}

func renderExecutableRules() *apparmorprofilev1alpha1.AppArmorExecutablesRules {
	return &apparmorprofilev1alpha1.AppArmorExecutablesRules{
		AllowedExecutables: &allPaths,
		AllowedLibraries:   &allPaths,
	}
}

func renderFileSystemRules() *apparmorprofilev1alpha1.AppArmorFsRules {
	return &apparmorprofilev1alpha1.AppArmorFsRules{
		ReadOnlyPaths:  &noWritePaths,
		ReadWritePaths: &allPaths,
	}
}

func renderNetworkRules() *apparmorprofilev1alpha1.AppArmorNetworkRules {
	return &apparmorprofilev1alpha1.AppArmorNetworkRules{
		AllowRaw: &[]bool{true}[0],
		Protocols: &apparmorprofilev1alpha1.AppArmorAllowedProtocols{
			AllowTCP: &[]bool{true}[0],
			AllowUDP: &[]bool{true}[0],
		},
	}
}

func renderCapabilityRules() *apparmorprofilev1alpha1.AppArmorCapabilityRules {
	return &apparmorprofilev1alpha1.AppArmorCapabilityRules{
		AllowedCapabilities: []string{
			"CAP_CHOWN",
			"CAP_DAC_OVERRIDE",
			"CAP_FOWNER",
			"CAP_FSETID",
			"CAP_SETGID",
			"CAP_SETUID",
			"CAP_NET_BIND_SERVICE",
			"CAP_SYS_CHROOT",
			"CAP_SYS_ADMIN",
			"CAP_SYS_PTRACE",
			"CAP_KILL",
			"CAP_AUDIT_WRITE",
			"CAP_SETFCAP",
		},
	}
}

var allPaths = []string{
	"/**",
}

var noWritePaths = []string{
	"/etc",
	"/usr",
	"/var",
	"/usr/lib/x86_64-linux-gnu/libEGL_*",
	"/usr/lib/x86_64-linux-gnu/libGLES*",
	"/usr/lib/x86_64-linux-gnu/libGLX_nvidia*",
	"/usr/lib/x86_64-linux-gnu/libnvcuvid*",
	"/usr/lib/x86_64-linux-gnu/gbm/nvidia-*",
	"/usr/lib/x86_64-linux-gnu/nvidia/wine/_nvngx.dll",
	"/usr/lib/x86_64-linux-gnu/nvidia/wine/nvngx.dll",
	"/usr/lib/x86_64-linux-gnu/nvidia/xorg/libglxserver_nvidia*",
	"/usr/lib/x86_64-linux-gnu/nvidia/xorg/nvidia_drv.so",
	"/usr/lib/x86_64-linux-gnu/vdpau/libvdpau_nvidia",
	"/usr/lib/x86_64-linux-gnu/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/lib/x86_64-linux-gnu/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/lib/x86_64-linux-gnu/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/lib/x86_64-linux-gnu/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/lib/x86_64-linux-gnu/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/lib/x86_64-linux-gnu/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/lib/x86_64-linux-gnu/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/lib/x86_64-linux-gnu/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/lib/x86_64-linux-gnu/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/nvidia/lib/x86_64-linux-gnu/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/nvidia/lib/x86_64-linux-gnu/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/nvidia/lib/x86_64-linux-gnu/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/nvidia/lib64/libnvidia-*.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/nvidia/lib64/libcuda.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/local/nvidia/lib64/libcudadebugger.so.[0-9][0-9][0-9].[0-9][0-9].[0-9]*",
	"/usr/bin/nvidia-smi",
	"/usr/bin/nvidia-debugdump",
	"/usr/bin/nvidia-persistenced",
	"/usr/bin/nv-fabricmanager",
	"/usr/bin/nvidia-cuda-mps-control",
	"/usr/bin/nvidia-cuda-mps-server",
	"/lib/firmware/nvidia/**/gsp_*.bin",
}
