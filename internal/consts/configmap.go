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
	ConfigMapKeySlurmBaseConfig         = "slurm_base.conf.noedit"
	ConfigMapKeyRESTConfig              = "slurm_rest.conf"
	ConfigMapKeySlurmK8sExtraConfig     = "slurm_k8s_extra.conf.noedit"
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

	// ResourceDistribution names
	ResourceDistributionNameTopology = "topology-soperator"
)
