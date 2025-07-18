package consts

const (
	slurmConfigs = slurmPrefix + "configs"
)

const (
	ConfigMapNameSlurmConfigs      = slurmConfigs
	ConfigMapNameSSHDConfigsLogin  = sshConfigs
	ConfigMapNameSSHDConfigsWorker = sshConfigsWorker
	ConfigMapNameSshRootPublicKeys = sshRootKeys
	ConfigMapNameSecurityLimits    = securityLimits
	ConfigMapNameSysctl            = sysctl
	ConfigMapNameSupervisord       = supervisord

	ConfigMapKeySlurmConfig             = "slurm.conf"
	ConfigMapKeyRESTConfig              = "slurm_rest.conf"
	ConfigMapKeyCustomSlurmConfig       = "custom_slurm.conf"
	ConfigMapKeyCGroupConfig            = "cgroup.conf"
	ConfigMapKeySpankConfig             = "plugstack.conf"
	ConfigMapKeyGresConfig              = "gres.conf"
	ConfigMapKeyMPIConfig               = "mpi.conf"
	ConfigMapKeySlurmdbdConfig          = "slurmdbd.conf"
	ConfigMapKeyTopologyConfig          = "topology.conf"
	ConfigMapKeyActiveCheckPrologScript = "activecheck-prolog.sh"

	ConfigMapKeySshdConfig              = SshdName + "_config"
	ConfigMapKeySshRootPublicKeysConfig = authorizedKeys
	ConfigMapKeySecurityLimits          = securityLimitsConfFile
	ConfigMapKeySysctl                  = sysctlConfFile
	ConfigMapKeySupervisord             = supervisordConfFile
	ConfigMapKeySoperatorcheckSbatch    = "sbatch.sh"

	ConfigMapNameTopologyNodeLabels      = "topology-node-labels"
	ConfigMapNameTopologyConfig          = "topology-config"
	ConfigMapNameActiveCheckPrologScript = "activecheck-prolog"
)
