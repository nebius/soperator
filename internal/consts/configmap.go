package consts

const (
	slurmConfigs   = slurmPrefix + "configs"
	slurmdbdSecret = "slurmdb-secret"
)

const (
	ConfigMapNameSlurmConfigs      = slurmConfigs
	ConfigMapNameSSHConfigs        = sshConfigs
	ConfigMapNameSshRootPublicKeys = sshRootKeys
	ConfigMapNameSecurityLimits    = securityLimits
	ConfigMapNameNCCLTopology      = ncclTopology
	ConfigMapNameSysctl            = sysctl

	ConfigMapKeySlurmConfig    = "slurm.conf"
	ConfigMapKeyCGroupConfig   = "cgroup.conf"
	ConfigMapKeySpankConfig    = "plugstack.conf"
	ConfigMapKeyGresConfig     = "gres.conf"
	ConfigMapKeySlurmdbdConfig = "slurmdbd.conf"

	ConfigMapKeySshdConfig              = SshdName + "_config"
	ConfigMapKeySshRootPublicKeysConfig = authorizedKeys
	ConfigMapKeySecurityLimits          = securityLimitsConfFile
	ConfigMapKeyNCCLTopology            = "virtualTopology.xml"
	ConfigMapKeySysctl                  = sysctlConfFile
)
