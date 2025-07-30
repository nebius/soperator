package common

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderConfigMapSlurmConfigs renders new [corev1.ConfigMap] containing '.conf' files for the following components:
//
// [consts.ConfigMapKeySlurmConfig] - Slurm config
// [consts.ConfigMapKeyCGroupConfig] - Cgroup config
// [consts.ConfigMapKeySpankConfig] - SPANK plugins config
// [consts.ConfigMapKeyGresConfig] - GRES config
// [consts.ConfigMapKeyMPIConfig] - PMIx config
func RenderConfigMapSlurmConfigs(cluster *values.SlurmCluster) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSlurmConfigsName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    RenderLabels(consts.ComponentTypeController, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySlurmConfig:       generateSlurmConfig(cluster).Render(),
			consts.ConfigMapKeyRESTConfig:        generateRESTConfig().Render(),
			consts.ConfigMapKeyCustomSlurmConfig: generateCustomSlurmConfig(cluster).Render(),
			consts.ConfigMapKeyCGroupConfig:      generateCGroupConfig(cluster).Render(),
			consts.ConfigMapKeySpankConfig:       generateSpankConfig(cluster).Render(),
			consts.ConfigMapKeyGresConfig:        generateGresConfig(cluster.ClusterType).Render(),
			consts.ConfigMapKeyMPIConfig:         generateMPIConfig(cluster).Render(),
		},
	}
}

// RenderConfigMapSlurmConfigs renders new [slurmv1alpha1.JailedConfig] for every config in `RenderConfigMapSlurmConfigs` result
func RenderJailedConfigSlurmConfigs(cluster *values.SlurmCluster) slurmv1alpha1.JailedConfig {
	// This must match ConfigMap name in `RenderConfigMapSlurmConfigs`
	name := naming.BuildConfigMapSlurmConfigsName(cluster.Name)

	return slurmv1alpha1.JailedConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSlurmConfigsName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    RenderLabels(consts.ComponentTypeController, cluster.Name),
		},
		Spec: slurmv1alpha1.JailedConfigSpec{
			ConfigMap: slurmv1alpha1.ConfigMapReference{
				Name: name,
			},
			Items: []corev1.KeyToPath{
				{Key: consts.ConfigMapKeySlurmConfig, Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeySlurmConfig)},
				{Key: consts.ConfigMapKeyRESTConfig, Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyRESTConfig)},
				{Key: consts.ConfigMapKeyCustomSlurmConfig, Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyCustomSlurmConfig)},
				{Key: consts.ConfigMapKeyCGroupConfig, Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyCGroupConfig)},
				{Key: consts.ConfigMapKeySpankConfig, Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeySpankConfig)},
				{Key: consts.ConfigMapKeyGresConfig, Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyGresConfig)},
				{Key: consts.ConfigMapKeyMPIConfig, Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyMPIConfig)},
			},
			UpdateActions: []slurmv1alpha1.UpdateAction{slurmv1alpha1.UpdateActionReconfigure},
		},
	}
}

func generateSlurmConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}

	res.AddProperty("ClusterName", cluster.Name)
	res.AddComment("")
	// example: SlurmctldHost=controller-0(controller-0)
	// beause in kubernetes, dns uses the dot notation to separate the service name and the namespace
	for i := int32(0); i < cluster.NodeController.Size; i++ {
		svcName := fmt.Sprintf("%s-%d", cluster.NodeController.StatefulSet.Name, i)
		res.AddProperty("SlurmctldHost", fmt.Sprintf("%s(%s)", svcName, svcName))
	}
	res.AddComment("")
	res.AddProperty("AuthType", "auth/"+consts.Munge)
	res.AddProperty("CredType", "cred/"+consts.Munge)
	res.AddComment("")
	res.AddComment("SlurmConfig Spec")
	addSlurmConfigProperties(res, cluster.SlurmConfig)
	res.AddComment("")
	if cluster.ClusterType == consts.ClusterTypeGPU {
		res.AddProperty("GresTypes", "gpu")
	}
	res.AddProperty("MpiDefault", "pmix")
	res.AddProperty("MailProg", "/usr/bin/true")
	res.AddProperty("PluginDir", "/usr/lib/x86_64-linux-gnu/"+consts.Slurm)
	res.AddProperty("ProctrackType", "proctrack/cgroup")
	res.AddProperty("ReturnToService", 2)
	res.AddComment("")
	res.AddProperty("SlurmctldPidFile", "/var/run/"+consts.SlurmctldName+".pid")
	res.AddProperty("SlurmctldPort", cluster.NodeController.ContainerSlurmctld.Port)
	res.AddComment("")
	res.AddProperty("SlurmdPidFile", "/var/run/"+consts.SlurmdName+".pid")
	res.AddProperty("SlurmdPort", cluster.NodeWorker.ContainerSlurmd.Port)
	res.AddComment("")
	res.AddProperty("SlurmdSpoolDir", naming.BuildVolumeMountSpoolPath(consts.SlurmdName))
	res.AddComment("")
	res.AddProperty("SlurmUser", consts.SlurmUser)
	res.AddComment("")
	res.AddProperty("StateSaveLocation", naming.BuildVolumeMountSpoolPath(consts.SlurmctldName))
	res.AddComment("")
	res.AddProperty("TaskPlugin", "task/cgroup,task/affinity")
	res.AddComment("")
	res.AddProperty("CliFilterPlugins", "cli_filter/user_defaults")
	res.AddComment("")
	res.AddProperty("LaunchParameters", "use_interactive_step")
	res.AddComment("Scrontab")
	res.AddProperty("ScronParameters", "enable,explicit_scancel")
	res.AddComment("")
	res.AddProperty("PropagateResourceLimits", "NONE") // Don't propagate ulimits from the login node by default
	res.AddComment("")
	res.AddProperty("SchedulerParameters", "nohold_on_prolog_fail,extra_constraints")
	res.AddComment("")
	res.AddComment("HEALTH CHECKS")
	res.AddComment("https://slurm.schedmd.com/slurm.conf.html#OPT_HealthCheckInterval")
	if cluster.HealthCheckConfig != nil {
		res.AddProperty("HealthCheckInterval", cluster.HealthCheckConfig.HealthCheckInterval)
		res.AddProperty("HealthCheckProgram", cluster.HealthCheckConfig.HealthCheckProgram)

		var states []string
		for _, state := range cluster.HealthCheckConfig.HealthCheckNodeState {
			states = append(states, state.State)
		}
		res.AddProperty("HealthCheckNodeState", strings.Join(states, ","))
	}
	res.AddComment("")
	res.AddProperty("InactiveLimit", 0)
	res.AddProperty("KillOnBadExit", 1)
	res.AddProperty("KillWait", 180)
	res.AddProperty("UnkillableStepTimeout", 600)
	res.AddProperty("SlurmctldTimeout", 30)
	res.AddProperty("SlurmdTimeout", 180)
	res.AddProperty("TCPTimeout", 15)
	res.AddProperty("WaitTime", 0)
	res.AddProperty("SlurmctldParameters", "conmgr_max_connections=512,conmgr_threads=16")
	res.AddComment("")
	res.AddComment("SCHEDULING")
	res.AddProperty("SchedulerType", "sched/backfill")
	res.AddProperty("SelectType", "select/cons_tres")
	res.AddProperty("SelectTypeParameters", "CR_Core_Memory,CR_CORE_DEFAULT_DIST_BLOCK")
	res.AddComment("")
	res.AddComment("LOGGING")
	res.AddProperty("SlurmctldDebug", consts.SlurmDefaultDebugLevel)
	res.AddProperty("SlurmctldLogFile", consts.SlurmLogFile)
	res.AddProperty("SlurmdDebug", consts.SlurmDefaultDebugLevel)
	res.AddProperty("SlurmdLogFile", consts.SlurmLogFile)
	res.AddProperty("DebugFlags", "Script")
	res.AddComment("")
	res.AddComment("COMPUTE NODES")
	res.AddComment("We're using the \"dynamic nodes\" feature: https://slurm.schedmd.com/dynamic_nodes.html")
	res.AddProperty("MaxNodeCount", "1024")
	res.AddProperty("MaxArraySize", "1024")
	res.AddProperty("JobRequeue", 1)
	res.AddProperty("PreemptMode", "REQUEUE")
	res.AddProperty("PreemptType", "preempt/partition_prio")
	res.AddComment("Partition Configuration")
	switch cluster.PartitionConfiguration.ConfigType {
	case "custom":
		for _, l := range cluster.PartitionConfiguration.RawConfig {
			line := strings.TrimSpace(l)
			if strings.HasPrefix(line, "PartitionName") {
				clearLine := strings.Replace(line, "PartitionName=", "", 1)
				res.AddProperty("PartitionName", clearLine)
			}
		}
	default:
		res.AddProperty("PartitionName", "main Nodes=ALL Default=YES PriorityTier=10 MaxTime=INFINITE State=UP OverSubscribe=YES")
		res.AddProperty("PartitionName", "background Nodes=ALL Default=NO PriorityTier=1 PreemptMode=OFF Hidden=YES MaxTime=INFINITE State=UP OverSubscribe=YES")
	}

	res.AddComment("")
	res.AddComment("Nodesets")
	for _, feature := range cluster.WorkerFeatures {
		if feature.NodesetName != "" {
			res.AddProperty("Nodeset", fmt.Sprintf("%s Feature=%s", feature.NodesetName, feature.Name))
		}
	}

	if cluster.NodeAccounting.Enabled {
		res.AddComment("")
		res.AddComment("ACCOUNTING")
		res.AddProperty("AccountingStorageType", "accounting_storage/slurmdbd")
		res.AddProperty("AccountingStorageHost", fmt.Sprintf(
			"%s.%s.svc.cluster.local",
			naming.BuildServiceName(consts.ComponentTypeAccounting, cluster.Name),
			cluster.Namespace,
		))
		res.AddProperty("AccountingStorageUser", consts.HostnameAccounting)
		res.AddProperty("AccountingStoragePort", consts.DefaultAccountingPort)
		res.AddProperty("JobCompType", "jobcomp/none")

		// In slurm.conf, the accounting section has many optional values
		// that can be added or removed, and to avoid writing many if statements, we decided to use a reflector.
		addSlurmConfigProperties(res, cluster.NodeAccounting.SlurmConfig)

		if cluster.NodeRest.Enabled {
			res.AddComment("")
			res.AddComment("REST API")
			res.AddProperty("AuthAltTypes", "auth/jwt")
			res.AddProperty("AuthAltParameters", "jwt_key="+consts.RESTJWTKeyPath)
		}
	}

	res.AddComment("")
	res.AddComment(fmt.Sprintf("Include %s", consts.ConfigMapKeyCustomSlurmConfig))
	res.AddPropertyWithConnector("include", consts.ConfigMapKeyCustomSlurmConfig, renderutils.SpaceConnector)

	return res
}

func generateCustomSlurmConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	multilineCfg := &renderutils.MultilineStringConfig{}
	multilineCfg.AddLine("# CUSTOM SLURM CONFIG")
	if cluster.CustomSlurmConfig != nil {
		multilineCfg.AddLine(*cluster.CustomSlurmConfig)
	}
	return multilineCfg
}

// addSlurmConfigProperties adds properties from the given struct to the config file
func addSlurmConfigProperties(res *renderutils.PropertiesConfig, config interface{}) {
	v := reflect.ValueOf(config)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldName := t.Field(i).Name

		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				continue
			}
			field = field.Elem()
		}

		if field.Kind() == reflect.String {
			if field.String() != "" {
				res.AddProperty(fieldName, field.String())
			}
			continue
		}

		if field.Kind() == reflect.Int32 || field.Kind() == reflect.Int16 {
			res.AddProperty(fieldName, field.Int())
			continue
		}
	}
}

func generateCGroupConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	res.AddProperty("CgroupMountpoint", "/sys/fs/cgroup")
	res.AddProperty("ConstrainCores", "yes")
	res.AddProperty("ConstrainDevices", "yes")
	res.AddProperty("ConstrainRAMSpace", "yes")
	switch cluster.NodeWorker.CgroupVersion {
	case consts.CGroupV1:
		res.AddProperty("CgroupPlugin", "cgroup/v1")
		res.AddProperty("ConstrainSwapSpace", "yes")
	case consts.CGroupV2:
		res.AddProperty("CgroupPlugin", "cgroup/v2")
		res.AddProperty("ConstrainSwapSpace", "no")
		res.AddProperty("EnableControllers", "yes")
		res.AddProperty("IgnoreSystemd", "yes")
	}
	return res
}

func generateSpankConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}

	res.AddLine(fmt.Sprintf("required chroot.so %s", consts.VolumeMountPathJail))

	// TODO(@itechdima): make `expose_enroot_logs` configurable and enable it once #413 is resolved.
	res.AddLine(strings.Join(
		[]string{
			utils.Ternary(cluster.PlugStackConfig.Pyxis.Required, "required", "optional"),
			"spank_pyxis.so",
			"runtime_path=/run/pyxis",
			"execute_entrypoint=0",
			"container_scope=global",
			"sbatch_support=1",
			fmt.Sprintf("container_image_save=%s", cluster.PlugStackConfig.Pyxis.ContainerImageSave),
		},
		" ",
	))

	{
		opts := cluster.PlugStackConfig.NcclDebug.DeepCopy()
		res.AddLine(strings.Join(
			[]string{
				utils.Ternary(opts.Required, "required", "optional"),
				"spanknccldebug.so",
				fmt.Sprintf("enabled=%d", utils.Ternary(opts.Enabled, 1, 0)),
				fmt.Sprintf("log-level=%s", utils.Ternary(opts.LogLevel != "", opts.LogLevel, "INFO")),
				fmt.Sprintf("out-file=%d", utils.Ternary(opts.OutputToFile, 1, 0)),
				fmt.Sprintf("out-dir=%s", utils.Ternary(opts.OutputDirectory != "", opts.OutputDirectory, "/opt/soperator-outputs/nccl_logs")),
				fmt.Sprintf("out-stdout=%d", utils.Ternary(opts.OutputToStdOut, 1, 0)),
			},
			" ",
		))
	}

	for _, plugin := range cluster.PlugStackConfig.CustomPlugins {
		conf := []string{
			utils.Ternary(plugin.Required, "required", "optional"),
			plugin.Path,
		}

		if len(plugin.Arguments) > 0 {
			for k, v := range plugin.Arguments {
				conf = append(conf, fmt.Sprintf("%s=%s", k, v))
			}
		}

		res.AddLine(strings.Join(conf, " "))
	}

	return res
}

func generateGresConfig(clusterType consts.ClusterType) renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	res.AddComment("Gres config")
	if clusterType == consts.ClusterTypeGPU {
		res.AddProperty("AutoDetect", "nvml")
	}
	return res
}

func generateMPIConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	res.AddComment("PMIx config")
	if cluster.MPIConfig.PMIxEnv != "" {
		res.AddProperty("PMIxEnv", cluster.MPIConfig.PMIxEnv)
	}
	return res
}

func generateRESTConfig() renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	res.AddComment("REST API config")
	res.AddPropertyWithConnector("include", consts.ConfigMapKeySlurmConfig, renderutils.SpaceConnector)
	res.AddProperty("AuthType", "auth/jwt")
	return res
}

// region Security limits

// RenderConfigMapSecurityLimits renders new [corev1.ConfigMap] containing security limits config file
func RenderConfigMapSecurityLimits(componentType consts.ComponentType, cluster *values.SlurmCluster) corev1.ConfigMap {
	var data string
	switch componentType {
	case consts.ComponentTypeLogin:
		data = cluster.NodeLogin.ContainerSshd.NodeContainer.SecurityLimitsConfig
	case consts.ComponentTypeWorker:
		data = cluster.NodeWorker.ContainerSlurmd.NodeContainer.SecurityLimitsConfig
		if data == "" {
			data = generateUnlimitedSecurityLimitsConfig().Render()
		}
	case consts.ComponentTypeController:
		data = cluster.NodeController.ContainerSlurmctld.NodeContainer.SecurityLimitsConfig
	}

	if data == "" {
		data = generateEmptySecurityLimitsConfig().Render()
	}

	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSecurityLimitsName(componentType, cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    RenderLabels(componentType, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySecurityLimits: data,
		},
	}
}

func generateUnlimitedSecurityLimitsConfig() renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine("# Set core file size to unlimited (-c)")
	res.AddLine("*    soft    core        unlimited")
	res.AddLine("*    hard    core        unlimited")
	res.AddLine("# Set data segment size to unlimited (-d)")
	res.AddLine("*    soft    data        unlimited")
	res.AddLine("*    hard    data        unlimited")
	res.AddLine("# Set file size to unlimited (-f)")
	res.AddLine("*    soft    fsize       unlimited")
	res.AddLine("*    hard    fsize       unlimited")
	res.AddLine("# Set pending signals to unlimited (-i)")
	res.AddLine("*    soft    sigpending  unlimited")
	res.AddLine("*    hard    sigpending  unlimited")
	res.AddLine("# Set locked-in-memory size to unlimited (-l)")
	res.AddLine("*    soft    memlock     unlimited")
	res.AddLine("*    hard    memlock     unlimited")
	res.AddLine("# Set resident set size (physical memory usage) to unlimited (-m)")
	res.AddLine("*    soft    rss         unlimited")
	res.AddLine("*    hard    rss         unlimited")
	res.AddLine("# Set the number of open files to 1048576 (-n)")
	res.AddLine("*    soft    nofile      1048576")
	res.AddLine("*    hard    nofile      1048576")
	res.AddLine("# Set POSIX message queue size to unlimited (-q)")
	res.AddLine("*    soft    msgqueue    unlimited")
	res.AddLine("*    hard    msgqueue    unlimited")
	res.AddLine("# Set real-time priority to unlimited (-r)")
	res.AddLine("*    soft    rtprio      unlimited")
	res.AddLine("*    hard    rtprio      unlimited")
	res.AddLine("# Set stack size to unlimited (-s)")
	res.AddLine("*    soft    stack       unlimited")
	res.AddLine("*    hard    stack       unlimited")
	res.AddLine("# Set CPU time to unlimited (-t)")
	res.AddLine("*    soft    cpu         unlimited")
	res.AddLine("*    hard    cpu         unlimited")
	res.AddLine("# Set the number of user processes to unlimited (-u)")
	res.AddLine("*    soft    nproc       unlimited")
	res.AddLine("*    hard    nproc       unlimited")
	res.AddLine("# Set virtual memory size to unlimited (-v)")
	res.AddLine("*    soft    as          unlimited")
	res.AddLine("*    hard    as          unlimited")
	res.AddLine("# Set the number of file locks to unlimited (-x)")
	res.AddLine("*    soft    locks       unlimited")
	res.AddLine("*    hard    locks       unlimited")
	res.AddLine("# Set max scheduling priority to -20 (-e)")
	res.AddLine("*    soft    nice        -20")
	res.AddLine("*    hard    nice        -20")
	return res
}

func generateEmptySecurityLimitsConfig() renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine("#Empty security limits file")
	return res
}

// endregion Security limits
