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
	ConfigMapNameUserIsolation     = userIsolation
	ConfigMapNameSysctl            = sysctl
	ConfigMapNamePAMSlurmAdopt     = pamSlurmAdopt

	ConfigMapKeySlurmConfig         = "slurm.conf"
	ConfigMapKeySlurmBaseConfig     = "slurm_base.conf.noedit"
	ConfigMapKeyRESTConfig          = "slurm_rest.conf"
	ConfigMapKeySlurmK8sExtraConfig = "slurm_k8s_extra.conf.noedit"
	ConfigMapKeyCGroupConfig        = "cgroup.conf"
	ConfigMapKeySpankConfig         = "plugstack.conf"
	ConfigMapKeyGresConfig          = "gres.conf"
	ConfigMapKeyMPIConfig           = "mpi.conf"
	ConfigMapKeyNamespaceConfig     = "namespace.yaml"
	ConfigMapKeySlurmdbdConfig      = "slurmdbd.conf"
	ConfigMapKeyTopologyYAML        = "topology.yaml"

	ConfigMapKeySshdConfig              = SshdName + "_config"
	ConfigMapKeySshRootPublicKeysConfig = authorizedKeys
	ConfigMapKeySecurityLimits          = securityLimitsConfFile
	ConfigMapKeyUserIsolation           = userIsolationConfFile
	ConfigMapKeySysctl                  = sysctlConfFile
	ConfigMapKeyPAMSlurmAdopt           = pamSlurmAdoptConfFile
	ConfigMapKeyPAMSlurmAdoptUsers      = pamSlurmAdoptUsersConfFile
	ConfigMapKeyPAMSlurmAdoptGroups     = pamSlurmAdoptGroupsConfFile
	ConfigMapKeySoperatorcheckSbatch    = "sbatch.sh"

	ConfigMapNameTopologyNodeLabels = "topology-node-labels"
	ConfigMapNameTopologyConfig     = "topology-config"

	// ResourceDistribution names
	ResourceDistributionNameTopology = "topology-soperator"
)
