package consts

const (
	slurmConfigs = slurmPrefix + "configs"
)

const (
	ConfigMapNameSlurmConfigs = slurmConfigs
	ConfigMapNameSSHConfigs   = sshConfigs

	ConfigMapKeySlurmConfig  = "slurm.conf"
	ConfigMapKeyCGroupConfig = "cgroup.conf"
	ConfigMapKeySpankConfig  = "plugstack.conf"
	ConfigMapKeyGresConfig   = "gres.conf"

	ConfigMapKeySshdConfig = SshdName + "_config"
)
