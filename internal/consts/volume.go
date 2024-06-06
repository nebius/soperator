package consts

const (
	spool = "spool"
	jail  = "jail"

	Munge       = "munge"
	mungePrefix = Munge + "-"
	mungeKey    = mungePrefix + "key"

	nvidia = "nvidia"
	boot   = "boot"
)

const (
	VolumeNameSlurmConfigs = slurmConfigs
	VolumeNameSpool        = spool
	VolumeNameJail         = jail
	VolumeNameMungeSocket  = mungePrefix + "socket"
	VolumeNameMungeKey     = mungeKey
	VolumeNameNvidia       = nvidia
	VolumeNameBoot         = boot

	VolumeMountPathSlurmConfigs = "/mnt/" + slurmConfigs
	VolumeMountPathSpool        = "/var/" + spool
	VolumeMountPathJail         = "/mnt/" + jail
	VolumeMountPathMungeSocket  = "/run/" + Munge
	VolumeMountPathMungeKey     = "/mnt/" + mungeKey
	VolumeMountPathNvidia       = "/run/" + nvidia
	VolumeMountPathBoot         = "/" + boot
)
