package consts

const (
	spool = "spool"
	jail  = "jail"

	Munge       = "munge"
	mungePrefix = Munge + "-"
	mungeKey    = mungePrefix + "key"
)

const (
	VolumeNameSlurmConfigs = slurmConfigs
	VolumeNameSpool        = spool
	VolumeNameJail         = jail
	VolumeNameMungeSocket  = mungePrefix + "socket"
	VolumeNameMungeKey     = mungeKey

	VolumeMountPathSlurmConfigs = "/mnt/" + slurmConfigs
	VolumeMountPathSpool        = "/var/" + spool
	VolumeMountPathJail         = "/mnt/" + jail
	VolumeMountPathMungeSocket  = "/run/" + Munge
	VolumeMountPathMungeKey     = "/mnt/" + mungeKey
)
