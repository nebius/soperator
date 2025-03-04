package consts

const (
	slurmConfigs   = slurmPrefix + "configs"
	slurmdbdSecret = "slurm-secrets"
)

const (
	ConfigMapNameSlurmConfigs      = slurmConfigs
	ConfigMapNameSSHDConfigs       = sshConfigs
	ConfigMapNameSshRootPublicKeys = sshRootKeys
	ConfigMapNameSecurityLimits    = securityLimits
	ConfigMapNameNCCLTopology      = ncclTopology
	ConfigMapNameSysctl            = sysctl
	ConfigMapNameSupervisord       = supervisord

	ConfigMapKeySlurmConfig       = "slurm.conf"
	ConfigMapKeyCustomSlurmConfig = "custom_slurm.conf"
	ConfigMapKeyCGroupConfig      = "cgroup.conf"
	ConfigMapKeySpankConfig       = "plugstack.conf"
	ConfigMapKeyGresConfig        = "gres.conf"
	ConfigMapKeyMPIConfig         = "mpi.conf"
	ConfigMapKeySlurmdbdConfig    = "slurmdbd.conf"

	ConfigMapKeySshdConfig              = SshdName + "_config"
	ConfigMapKeySshRootPublicKeysConfig = authorizedKeys
	ConfigMapKeySecurityLimits          = securityLimitsConfFile
	ConfigMapKeyNCCLTopology            = "virtualTopology.xml"
	ConfigMapKeySysctl                  = sysctlConfFile
	ConfigMapKeySupervisord             = supervisordConfFile
)
