package common

import (
	"fmt"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"
)

func GenerateSlurmConfig(cluster *values.SlurmCluster) ConfFile {
	res := &propertiesConfig{}

	res.addProperty("ClusterName", cluster.Name)
	res.addComment("")
	// example: SlurmctldHost=controller-0(controller-0.controller.slurm-poc.svc.cluster.local)
	for i := range cluster.NodeController.Size {
		replicaName, replicaFQDN := naming.BuildServiceReplicaFQDN(
			consts.ComponentTypeController,
			cluster.Namespace,
			cluster.Name,
			i,
		)
		res.addProperty("SlurmctldHost", fmt.Sprintf("%s(%s)", replicaName, replicaFQDN))
	}
	res.addComment("")
	res.addProperty("AuthType", "auth/"+consts.Munge)
	res.addProperty("CredType", "cred/"+consts.Munge)
	res.addComment("")
	res.addProperty("GresTypes", "gpu")
	res.addProperty("MailProg", "/usr/bin/true")
	res.addProperty("PluginDir", "/usr/local/lib/"+consts.Slurm)
	res.addProperty("ProctrackType", "proctrack/linuxproc")
	res.addProperty("ReturnToService", 1)
	res.addComment("")
	res.addProperty("SlurmctldPidFile", "/var/run/"+consts.SlurmctldName+".pid")
	res.addProperty("SlurmctldPort", cluster.NodeController.ContainerSlurmctld.Port)
	res.addComment("")
	res.addProperty("SlurmdPidFile", "/var/run/"+consts.SlurmdName+".pid")
	res.addProperty("SlurmdPort", cluster.NodeWorker.ContainerSlurmd.Port)
	res.addComment("")
	res.addProperty("SlurmdSpoolDir", naming.BuildVolumeMountSpoolPath(consts.SlurmdName))
	res.addComment("")
	res.addProperty("SlurmUser", "root")
	res.addComment("")
	res.addProperty("StateSaveLocation", naming.BuildVolumeMountSpoolPath(consts.SlurmctldName))
	res.addComment("")
	res.addProperty("TaskPlugin", "task/affinity")
	res.addComment("")
	res.addComment("HEALTH CHECKS")
	res.addComment("https://slurm.schedmd.com/slurm.conf.html#OPT_HealthCheckInterval")
	res.addProperty("HealthCheckInterval", 30)
	res.addProperty("HealthCheckProgram", "/usr/bin/gpu_healthcheck.sh")
	res.addProperty("HealthCheckNodeState", "ANY")
	res.addComment("")
	res.addProperty("InactiveLimit", 0)
	res.addProperty("KillWait", 30)
	res.addProperty("MinJobAge", 300)
	res.addProperty("SlurmctldTimeout", 120)
	res.addProperty("SlurmdTimeout", 300)
	res.addProperty("Waittime", 0)
	res.addComment("")
	res.addComment("SCHEDULING")
	res.addProperty("SchedulerType", "sched/backfill")
	res.addProperty("SelectType", "select/cons_tres")
	res.addComment("")
	res.addComment("LOGGING AND ACCOUNTING")
	res.addProperty("JobCompType", "jobcomp/none")
	res.addProperty("JobAcctGatherFrequency", 30)
	res.addProperty("SlurmctldDebug", "debug3")
	res.addProperty("SlurmctldLogFile", "/var/log/"+consts.SlurmctldName+".log")
	res.addProperty("SlurmdDebug", "debug3")
	res.addProperty("SlurmdLogFile", "/var/log/"+consts.SlurmdName+".log")
	res.addComment("")
	res.addComment("COMPUTE NODES")
	res.addComment("We're using the \"dynamic nodes\" feature: https://slurm.schedmd.com/dynamic_nodes.html")
	res.addProperty("MaxNodeCount", "512")
	res.addProperty("PartitionName", "main Nodes=ALL Default=YES MaxTime=INFINITE State=UP")

	return res
}
