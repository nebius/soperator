package consts

const (
	spool = "spool"
	jail  = "jail"

	Munge       = "munge"
	mungePrefix = Munge + "-"
	mungeKey    = mungePrefix + "key"

	RESTJWTKey = "rest-jwt-key"

	nvidia = "nvidia"
	boot   = "boot"

	sshConfigs             = "ssh-configs"
	sshRootKeys            = "ssh-root-keys"
	authorizedKeys         = "authorized_keys"
	securityLimits         = "security-limits"
	securityLimitsConfFile = "limits.conf"

	ncclTopology          = "nccl-topology"
	sysctl                = "sysctl"
	sysctlConfFile        = sysctl + ".conf"
	supervisord           = "supervisord"
	unkillableStepProgram = "unkillable-step-program"
	supervisordConfFile   = supervisord + ".conf"
)

const (
	VolumeNameSlurmConfigs            = slurmConfigs
	VolumeNameSlurmdbdSecret          = slurmdbdSecret
	VolumeNameSpool                   = spool
	VolumeNameJail                    = jail
	VolumeNameJailSnapshot            = jail + "-snapshot"
	VolumeNameMungeSocket             = mungePrefix + "socket"
	VolumeNameMungeKey                = mungeKey
	VolumenameRESTJWTKey              = RESTJWTKey
	VolumeNameNvidia                  = nvidia
	VolumeNameBoot                    = boot
	VolumeNameSSHDConfigs             = sshConfigs
	VolumeNameSSHRootKeys             = sshRootKeys
	VolumeNameSSHDKeys                = "sshd-keys"
	VolumeMountPathSSHDKeys           = "/etc/ssh/sshd_keys"
	VolumeNameSecurityLimits          = securityLimits
	VolumeNameNCCLTopology            = ncclTopology
	VolumeNameSharedMemory            = "dev-shm"
	VolumeNameSysctl                  = sysctl
	VolumeNameSupervisordConfigMap    = "supervisord-config"
	VolumeNameUnkillableStepProgramCM = "unkillable-step-program"

	VolumeMountPathSlurmConfigs          = "/mnt/" + slurmConfigs
	VolumeMountPathSlurmdbdSecret        = "/mnt/" + slurmdbdSecret
	VolumeMountPathSpool                 = "/var/" + spool
	VolumeMountPathSpoolSlurmdbd         = "/var/spool/slurmdbd"
	VolumeMountPathJail                  = "/mnt/" + jail
	VolumeMountPathJailSnapshot          = "/jail"
	VolumeMountPathJailUpper             = "/mnt/" + jail + ".upper"
	VolumeMountPathMungeSocket           = "/run/" + Munge
	VolumeMountPathMungeKey              = "/mnt/" + mungeKey
	VolumeMountPathRESTJWTKey            = "/mnt/" + RESTJWTKey
	VolumeMountPathNvidia                = "/run/" + nvidia
	VolumeMountPathBoot                  = "/" + boot
	VolumeMountPathSSHConfigs            = "/mnt/" + sshConfigs
	VolumeMountPathSSHRootKeys           = "/root/.ssh/" + authorizedKeys
	VolumeMountSubPathSSHRootKeys        = authorizedKeys
	VolumeMountPathSecurityLimits        = "/etc/security/" + securityLimitsConfFile
	VolumeMountSubPathSecurityLimits     = securityLimitsConfFile
	VolumeMountPathNCCLTopology          = "/run/nvidia-topologyd"
	VolumeMountPathSharedMemory          = "/dev/shm"
	VolumeMountPathSysctl                = "/etc/" + sysctlConfFile
	VolumeMountSubPathSysctl             = sysctlConfFile
	VolumeMountPathSupervisordConfig     = "/etc/supervisor/conf.d/"
	VolumeMountPathUnkillableStepProgram = "/etc/unkillable-step-program/"
)
