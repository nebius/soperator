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
	ConfigMapNameSupervisord       = supervisord

	ConfigMapKeySlurmConfig         = "slurm.conf"
	ConfigMapKeySlurmBaseConfig     = "slurm_base.conf.noedit"
	ConfigMapKeyRESTConfig          = "slurm_rest.conf"
	ConfigMapKeySlurmK8sExtraConfig = "slurm_k8s_extra.conf.noedit"
	ConfigMapKeyCGroupConfig        = "cgroup.conf"
	ConfigMapKeySpankConfig         = "plugstack.conf"
	ConfigMapKeyGresConfig          = "gres.conf"
	ConfigMapKeyMPIConfig           = "mpi.conf"
	ConfigMapKeySlurmdbdConfig      = "slurmdbd.conf"
	// ConfigMapKeyTopologyYAML holds the cluster's named topologies.
	ConfigMapKeyTopologyYAML = "topology.yaml"

	ConfigMapKeySshdConfig              = SshdName + "_config"
	ConfigMapKeySshRootPublicKeysConfig = authorizedKeys
	ConfigMapKeySecurityLimits          = securityLimitsConfFile
	ConfigMapKeyUserIsolation           = userIsolationConfFile
	ConfigMapKeySysctl                  = sysctlConfFile
	ConfigMapKeySupervisord             = supervisordConfFile
	ConfigMapKeySoperatorcheckSbatch    = "sbatch.sh"

	// AnnotationTopologyStructure records the topology structure the JailedConfig was last rendered
	// for: which topologies exist, their plugins and block sizes. A change means slurmctld has to
	// re-read the config, which node membership changes do not.
	AnnotationTopologyStructure = "topology.slurm.nebius.ai/structure"

	// AnnotationTopologyAppliedHash records the JailedConfig's applied hash as it stood when the
	// current reconfigure request was raised. A reconfigure has reached slurmctld only once the
	// applied hash has moved past it. Generation cannot answer that: the request lives in the spec
	// but the structure lives in an annotation, so changing the structure while a request is
	// outstanding leaves Generation untouched, and a confirmation earned over the previous content
	// would otherwise read as confirming the new one.
	AnnotationTopologyAppliedHash = "topology.slurm.nebius.ai/applied-hash"

	ConfigMapNameTopologyNodeLabels = "topology-node-labels"
	ConfigMapNameTopologyConfig     = "topology-config"

	// ResourceDistribution names
	ResourceDistributionNameTopology = "topology-soperator"
)
