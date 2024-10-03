package common

import (
	"fmt"
	"reflect"

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
	if cluster.ClusterType == consts.ClusterTypeGPU {
		res.AddProperty("GresTypes", "gpu")
	}
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
	res.AddComment("")
	res.AddProperty("MaxJobCount", 1000) // Keep 1000 last jobs in controller memory
	res.AddProperty("MinJobAge", 86400)  // Don't remove jobs from controller memory after some time
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
	res.AddProperty("KillWait", 30)
	res.AddProperty("SlurmctldTimeout", 120)
	res.AddProperty("SlurmdTimeout", 300)
	res.AddProperty("Waittime", 0)
	res.AddComment("")
	res.AddComment("SCHEDULING")
	res.AddProperty("SchedulerType", "sched/backfill")
	res.AddProperty("SelectType", "select/cons_tres")
	res.AddProperty("SelectTypeParameters", "CR_Core_Memory")
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
	res.AddProperty("PartitionName", "main Nodes=ALL Default=YES MaxTime=INFINITE State=UP OverSubscribe=YES")
	if cluster.NodeAccounting.Enabled {
		res.AddComment("")
		res.AddComment("ACCOUNTING")
		res.AddProperty("AccountingStorageType", "accounting_storage/slurmdbd")
		res.AddProperty("AccountingStorageHost", naming.BuildServiceName(consts.ComponentTypeAccounting, cluster.Name))
		res.AddProperty("AccountingStorageUser", consts.HostnameAccounting)
		res.AddProperty("AccountingStoragePort", consts.DefaultAccountingPort)
		res.AddProperty("JobCompType", "jobcomp/none")

		// In slurm.conf, the accounting section has many optional values
		// that can be added or removed, and to avoid writing many if statements, we decided to use a reflector.
		v := reflect.ValueOf(cluster.NodeAccounting.SlurmConfig)
		typeOfS := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if !isZero(field) {
				key := typeOfS.Field(i).Name
				switch field.Kind() {
				case reflect.String:
					res.AddProperty(key, field.String())
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					res.AddProperty(key, field.Int())
				}

			}
		}
	}
	return res
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
	res.AddLine("required spank_pyxis.so runtime_path=/run/pyxis execute_entrypoint=0 container_scope=global sbatch_support=1")
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

func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.Len() == 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	}

	return false
}

// region Security limits

// RenderConfigMapSecurityLimits renders new [corev1.ConfigMap] containing security limits config file
func RenderConfigMapSecurityLimits(componentType consts.ComponentType, cluster *values.SlurmCluster) corev1.ConfigMap {
	var data string
	switch componentType {
	case consts.ComponentTypeLogin:
		data = cluster.NodeLogin.ContainerSshd.NodeContainer.SecurityLimitsConfig
		if data == "" {
			data = generateDefaultSecurityLimitsConfig().Render()
		}
	case consts.ComponentTypeWorker:
		data = cluster.NodeWorker.ContainerSlurmd.NodeContainer.SecurityLimitsConfig
		if data == "" {
			data = generateDefaultSecurityLimitsConfig().Render()
		}
	case consts.ComponentTypeController:
		data = cluster.NodeController.ContainerSlurmctld.NodeContainer.SecurityLimitsConfig
	//case consts.ComponentTypeExporter:
	//	data = cluster.SlurmExporter.
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

func generateDefaultSecurityLimitsConfig() renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine("*       soft    memlock     unlimited")
	res.AddLine("*       hard    memlock     unlimited")
	res.AddLine("*       soft    nofile      1048576")
	res.AddLine("*       hard    nofile      1048576")
	return res
}

func generateEmptySecurityLimitsConfig() renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine("#Empty security limits file")
	return res
}

// endregion Security limits
