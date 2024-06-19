package consts

const (
	spool = "spool"
	jail  = "jail"

	Munge       = "munge"
	mungePrefix = Munge + "-"
	mungeKey    = mungePrefix + "key"

	nvidia = "nvidia"
	boot   = "boot"

	sshConfigs             = "ssh-configs"
	sshRootKeys            = "ssh-root-keys"
	authorizedKeys         = "authorized_keys"
	securityLimits         = "security-limits"
	securityLimitsConfFile = "limits.conf"

	ncclTopology   = "nccl-topology"
	sysctl         = "sysctl"
	sysctlConfFile = sysctl + ".conf"
)

const (
	VolumeNameSlurmConfigs   = slurmConfigs
	VolumeNameSpool          = spool
	VolumeNameJail           = jail
	VolumeNameMungeSocket    = mungePrefix + "socket"
	VolumeNameMungeKey       = mungeKey
	VolumeNameNvidia         = nvidia
	VolumeNameBoot           = boot
	VolumeNameSSHConfigs     = sshConfigs
	VolumeNameSSHRootKeys    = sshRootKeys
	VolumeNameSecurityLimits = securityLimits
	VolumeNameNCCLTopology   = ncclTopology
	VolumeNameSharedMemory   = "dev-shm"
	VolumeNameSysctl         = sysctl

	VolumeMountPathSlurmConfigs      = "/mnt/" + slurmConfigs
	VolumeMountPathSpool             = "/var/" + spool
	VolumeMountPathJail              = "/mnt/" + jail
	VolumeMountPathMungeSocket       = "/run/" + Munge
	VolumeMountPathMungeKey          = "/mnt/" + mungeKey
	VolumeMountPathNvidia            = "/run/" + nvidia
	VolumeMountPathBoot              = "/" + boot
	VolumeMountPathSSHConfigs        = "/mnt/" + sshConfigs
	VolumeMountPathSSHRootKeys       = "/root/.ssh/" + authorizedKeys
	VolumeMountSubPathSSHRootKeys    = authorizedKeys
	VolumeMountPathSecurityLimits    = "/etc/security/" + securityLimitsConfFile
	VolumeMountSubPathSecurityLimits = securityLimitsConfFile
	VolumeMountPathNCCLTopology      = "/run/nvidia-topologyd"
	VolumeMountPathSharedMemory      = "/dev/shm"
	VolumeMountPathSysctl            = "/etc/" + sysctlConfFile
	VolumeMountSubPathSysctl         = sysctlConfFile
)
