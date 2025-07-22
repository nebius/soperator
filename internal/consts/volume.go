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
	sshConfigsLogin        = "ssh-configs"
	sshConfigsWorker       = "ssh-configs-worker"
	sshRootKeys            = "ssh-root-keys"
	authorizedKeys         = "authorized_keys"
	securityLimits         = "security-limits"
	securityLimitsConfFile = "limits.conf"

	sysctl              = "sysctl"
	sysctlConfFile      = sysctl + ".conf"
	supervisord         = "supervisord"
	supervisordConfFile = supervisord + ".conf"

	slurmdbdSSLCACertificate = "slurmdbd-ssl-ca-cert"
	slurmdbdSSLClientKey     = "slurmdbd-ssl-client-key"
)

const (
	SlurmdbdConfFile = "slurmdbd.conf"

	VolumeNameSlurmConfigs             = slurmConfigs
	VolumeNameSpool                    = spool
	VolumeNameJail                     = jail
	VolumeNameJailSnapshot             = jail + "-snapshot"
	VolumeNameMungeSocket              = mungePrefix + "socket"
	VolumeNameMungeKey                 = mungeKey
	VolumenameRESTJWTKey               = RESTJWTKey
	VolumeNameNvidia                   = nvidia
	VolumeNameBoot                     = boot
	VolumeNameSSHDConfigsLogin         = sshConfigsLogin
	VolumeNameSSHDConfigsWorker        = sshConfigsWorker
	VolumeNameSSHRootKeys              = sshRootKeys
	VolumeNameSSHDKeys                 = "sshd-keys"
	VolumeMountPathSSHDKeys            = "/etc/ssh/sshd_keys"
	VolumeNameSecurityLimits           = securityLimits
	VolumeNameSharedMemory             = "dev-shm"
	VolumeNameSysctl                   = sysctl
	VolumeNameSupervisordConfigMap     = "supervisord-config"
	VolumeNameInMemorySubmount         = "in-memory"
	VolumeNameTmpDisk                  = "tmp-disk"
	VolumeNameSlurmdbdSSLCACertificate = "slurmdbd-ssl-ca-cert"
	VolumeNameSlurmdbdSSLClientKey     = "slurmdbd-ssl-client-key"

	VolumeMountPathSlurmConfigs             = "/mnt/" + slurmConfigs
	VolumeMountPathSpool                    = "/var/" + spool
	VolumeMountPathSpoolSlurmdbd            = "/var/spool/slurmdbd"
	VolumeMountPathJail                     = "/mnt/" + jail
	VolumeMountPathJailSnapshot             = "/jail"
	VolumeMountPathJailUpper                = "/mnt/" + jail + ".upper"
	VolumeMountPathMungeSocket              = "/run/" + Munge
	VolumeMountPathMungeKey                 = "/mnt/" + mungeKey
	VolumeMountPathRESTJWTKey               = "/mnt/" + RESTJWTKey
	VolumeMountPathNvidia                   = "/run/" + nvidia
	VolumeMountPathBoot                     = "/" + boot
	VolumeMountPathSSHConfigs               = "/mnt/" + sshConfigs
	VolumeMountPathSSHRootKeys              = "/root/.ssh/" + authorizedKeys
	VolumeMountSubPathSSHRootKeys           = authorizedKeys
	VolumeMountPathSecurityLimits           = "/etc/security/" + securityLimitsConfFile
	VolumeMountSubPathSecurityLimits        = securityLimitsConfFile
	VolumeMountPathSharedMemory             = "/dev/shm"
	VolumeMountPathSysctl                   = "/etc/" + sysctlConfFile
	VolumeMountSubPathSysctl                = sysctlConfFile
	VolumeMountPathSupervisordConfig        = "/etc/supervisor/conf.d/"
	VolumeMountPathInMemorySubmount         = VolumeMountPathJailUpper + "/mnt/memory"
	VolumeMountPathTmpDisk                  = "/tmp"
	VolumeMountPathSlurmdbdSSLCACertificate = "/mnt/" + slurmdbdSSLCACertificate
	VolumeMountPathSlurmdbdSSLClientKey     = "/mnt/" + slurmdbdSSLClientKey
)
