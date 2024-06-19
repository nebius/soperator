package consts

const (
	slurmConfigs = slurmPrefix + "configs"
)

const (
	ConfigMapNameSlurmConfigs   = slurmConfigs
	ConfigMapNameSSHConfigs     = sshConfigs
	ConfigMapNameSecurityLimits = securityLimits
	ConfigMapNameNCCLTopology   = ncclTopology
	ConfigMapNameSysctl         = sysctl

	ConfigMapKeySlurmConfig  = "slurm.conf"
	ConfigMapKeyCGroupConfig = "cgroup.conf"
	ConfigMapKeySpankConfig  = "plugstack.conf"
	ConfigMapKeyGresConfig   = "gres.conf"

	ConfigMapKeySshdConfig     = SshdName + "_config"
	ConfigMapKeySecurityLimits = securityLimitsConfFile
	ConfigMapKeyNCCLTopology   = "virtualTopology.xml"
	ConfigMapKeySysctl         = sysctlConfFile
)
