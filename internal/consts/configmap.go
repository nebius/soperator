package consts

const (
	slurmConfigs = slurmPrefix + "configs"
)

const (
	ConfigMapSlurmConfigsName = slurmConfigs

	ConfigMapSlurmConfigKey  = "slurm.conf"
	ConfigMapCGroupConfigKey = "cgroup.conf"
	ConfigMapSpankConfigKey  = "plugstack.conf"
	ConfigMapGresConfigKey   = "gres.conf"
)
