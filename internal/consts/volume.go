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
	VolumeNameSlurmdbdSecret = slurmdbdSecret
	VolumeNameSpool          = spool
	VolumeNameJail           = jail
	VolumeNameJailSnapshot   = jail + "-snapshot"
	VolumeNameMungeSocket    = mungePrefix + "socket"
	VolumeNameMungeKey       = mungeKey
	VolumeNameNvidia         = nvidia
	VolumeNameBoot           = boot
	VolumeNameSSHConfigs     = sshConfigs
	VolumeNameSSHRootKeys    = sshRootKeys
	VolumeNameSSHDKeys       = "sshd-keys"
	VolumeMountPathSSHDKeys  = "/etc/ssh/sshd_keys"
	VolumeNameSecurityLimits = securityLimits
	VolumeNameNCCLTopology   = ncclTopology
	VolumeNameSharedMemory   = "dev-shm"
	VolumeNameSysctl         = sysctl
	VolumeNameProc           = "proc"
	VolumeNameCgroup         = "cgroup"

	VolumeMountPathSlurmConfigs      = "/mnt/" + slurmConfigs
	VolumeMountPathSlurmdbdSecret    = "/mnt/" + slurmdbdSecret
	VolumeMountPathSpool             = "/var/" + spool
	VolumeMountPathSpoolSlurmdbd     = "/var/spool/slurmdbd"
	VolumeMountPathJail              = "/mnt/" + jail
	VolumeMountPathJailSnapshot      = "/jail"
	VolumeMountPathJailUpper         = "/mnt/" + jail + ".upper"
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
	VolumeMountPathProc              = "/proc"
	VolumeMountPathCgroup            = "/sys/fs/cgroup"
)
