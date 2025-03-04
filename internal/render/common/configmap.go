package common

import (
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderConfigMapSlurmConfigs renders new [corev1.ConfigMap] containing '.conf' files for the following components:
//
// [consts.ConfigMapKeySlurmConfig] - Slurm config
// [consts.ConfigMapKeyCGroupConfig] - cgroup config
// [consts.ConfigMapKeySpankConfig] - SPANK plugins config
// [consts.ConfigMapKeyGresConfig] - gres config
func RenderConfigMapSlurmConfigs(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSlurmConfigsName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    RenderLabels(consts.ComponentTypeController, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySlurmConfig:  generateSlurmConfig(cluster).Render(),
			consts.ConfigMapKeyCGroupConfig: generateCGroupConfig(cluster).Render(),
			consts.ConfigMapKeySpankConfig:  generateSpankConfig().Render(),
			consts.ConfigMapKeyGresConfig:   generateGresConfig(cluster.ClusterType).Render(),
			consts.ConfigMapKeyMPIConfig:    generateMPIConfig(cluster).Render(),
		},
	}, nil
}

func generateSlurmConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}

	res.AddProperty("ClusterName", cluster.Name)
	res.AddComment("")
	// example: SlurmctldHost=controller-0(controller-0.controller.slurm-poc.svc.cluster.local)
	for i := int32(0); i < cluster.NodeController.Size; i++ {
		hostName, hostFQDN := naming.BuildServiceHostFQDN(
			consts.ComponentTypeController,
			cluster.Namespace,
			cluster.Name,
			i,
		)
		res.AddProperty("SlurmctldHost", fmt.Sprintf("%s(%s)", hostName, hostFQDN))
	}
	res.AddComment("")
	res.AddProperty("AuthType", "auth/"+consts.Munge)
	res.AddProperty("CredType", "cred/"+consts.Munge)
	res.AddComment("")
	res.AddComment("SlurnConfig Spec")
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
	res.AddComment("HEALTH CHECKS")
	res.AddComment("https://slurm.schedmd.com/slurm.conf.html#OPT_HealthCheckInterval")
	res.AddProperty("HealthCheckInterval", 30)
	if cluster.ClusterType == consts.ClusterTypeGPU {
		res.AddProperty("HealthCheckProgram", "/usr/bin/gpu_healthcheck.sh")
	}
	res.AddProperty("HealthCheckNodeState", "ANY")
	res.AddComment("")
	res.AddProperty("InactiveLimit", 0)
	res.AddProperty("KillWait", 180)
	res.AddProperty("UnkillableStepTimeout", 600)
	res.AddProperty("SlurmctldTimeout", 30)
	res.AddProperty("SlurmdTimeout", 180)
	res.AddProperty("WaitTime", 0)
	res.AddComment("")
	res.AddComment("SCHEDULING")
	res.AddProperty("SchedulerType", "sched/backfill")
	res.AddProperty("SelectType", "select/cons_tres")
	res.AddProperty("SelectTypeParameters", "CR_Core_Memory,CR_CORE_DEFAULT_DIST_BLOCK") // TODO: Make it configurable
	res.AddComment("")
	res.AddComment("LOGGING")
	res.AddProperty("SlurmctldDebug", consts.SlurmDefaultDebugLevel)
	res.AddProperty("SlurmctldLogFile", consts.SlurmLogFile)
	res.AddProperty("SlurmdDebug", consts.SlurmDefaultDebugLevel)
	res.AddProperty("SlurmdLogFile", consts.SlurmLogFile)
	res.AddComment("")
	res.AddComment("COMPUTE NODES")
	res.AddComment("We're using the \"dynamic nodes\" feature: https://slurm.schedmd.com/dynamic_nodes.html")
	res.AddProperty("MaxNodeCount", "512")
	res.AddComment("Partition Configuration")
	res.AddProperty("JobRequeue", 1)
	res.AddProperty("PreemptMode", "REQUEUE")
	res.AddProperty("PreemptType", "preempt/partition_prio")
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
		res.AddProperty("PartitionName", "main Nodes=ALL Default=YES MaxTime=INFINITE State=UP OverSubscribe=YES")
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
	return res
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

func generateSpankConfig() renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine(fmt.Sprintf("required chroot.so %s", consts.VolumeMountPathJail))
	// TODO: make `container_image_save` and `expose_enroot_logs` configurable
	// TODO: enable `expose_enroot_logs` once #413 is resolved.
	res.AddLine("required spank_pyxis.so runtime_path=/run/pyxis execute_entrypoint=0 container_scope=global sbatch_support=1 container_image_save=/var/cache/enroot-container-images/")
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
	case consts.ComponentTypeBenchmark:
		data = cluster.NCCLBenchmark.ContainerNCCLBenchmark.NodeContainer.SecurityLimitsConfig
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

// region SSHD config

// RenderDefaultConfigMapSSHDConfigs renders new [corev1.ConfigMap] containing sshd config file
func RenderDefaultConfigMapSSHDConfigs(
	cluster *values.SlurmCluster,
	componentType consts.ComponentType,
) (corev1.ConfigMap, error) {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSSHDConfigsName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    RenderLabels(componentType, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySshdConfig: generateDefaultSshdConfig(cluster).Render(),
		},
	}, nil
}

func generateDefaultSshdConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine(fmt.Sprintf("Port %d", cluster.NodeLogin.ContainerSshd.Port))
	res.AddLine("PermitRootLogin yes")
	res.AddLine("PasswordAuthentication no")
	res.AddLine("ChallengeResponseAuthentication no")
	res.AddLine("UsePAM yes")
	res.AddLine("AcceptEnv LANG LC_*")
	res.AddLine("X11Forwarding no")
	res.AddLine("AllowTcpForwarding yes")
	res.AddLine("Subsystem sftp internal-sftp")
	res.AddLine("HostKey " + consts.VolumeMountPathSSHDKeys + "/" + consts.SecretSshdRSAKeyName)
	res.AddLine("HostKey " + consts.VolumeMountPathSSHDKeys + "/" + consts.SecretSshdECDSAKeyName)
	res.AddLine("HostKey " + consts.VolumeMountPathSSHDKeys + "/" + consts.SecretSshdECDSA25519KeyName)
	res.AddLine("ChrootDirectory " + consts.VolumeMountPathJail)
	res.AddLine("ClientAliveInterval " + consts.SSHDClientAliveInterval)
	res.AddLine("ClientAliveCountMax " + consts.SSHDClientAliveCountMax)
	res.AddLine("MaxStartups " + consts.SSHDMaxStartups)
	res.AddLine("LoginGraceTime " + consts.SSHDLoginGraceTime)
	res.AddLine("MaxAuthTries " + consts.SSHDMaxAuthTries)
	res.AddLine("LogLevel DEBUG3")
	res.AddLine("")
	res.AddLine("Match User root")
	res.AddLine("    AuthorizedKeysFile /root/.ssh/authorized_keys " + consts.VolumeMountPathJail + "/root/.ssh/authorized_keys")
	res.AddLine("")
	res.AddLine("Match User *")
	res.AddLine("    LogLevel INFO")
	return res
}

// endregion SSHD config
