package consts

const (
	spool = "spool"
	jail  = "jail"

	Munge       = "munge"
	mungePrefix = Munge + "-"
	mungeKey    = mungePrefix + "key"

	nvidia = "nvidia"
	boot   = "boot"

	sshConfigs     = "ssh-configs"
	sshRootKeys    = "ssh-root-keys"
	authorizedKeys = "authorized_keys"
)

const (
	VolumeNameSlurmConfigs = slurmConfigs
	VolumeNameSpool        = spool
	VolumeNameJail         = jail
	VolumeNameMungeSocket  = mungePrefix + "socket"
	VolumeNameMungeKey     = mungeKey
	VolumeNameNvidia       = nvidia
	VolumeNameBoot         = boot
	VolumeNameSSHConfigs   = sshConfigs
	VolumeNameSSHRootKeys  = sshRootKeys

	VolumeMountPathSlurmConfigs   = "/mnt/" + slurmConfigs
	VolumeMountPathSpool          = "/var/" + spool
	VolumeMountPathJail           = "/mnt/" + jail
	VolumeMountPathMungeSocket    = "/run/" + Munge
	VolumeMountPathMungeKey       = "/mnt/" + mungeKey
	VolumeMountPathNvidia         = "/run/" + nvidia
	VolumeMountPathBoot           = "/" + boot
	VolumeMountPathSSHConfigs     = "/mnt/" + sshConfigs
	VolumeMountPathSSHRootKeys    = "/root/.ssh/" + authorizedKeys
	VolumeMountSubPathSSHRootKeys = authorizedKeys
)
