package common

import (
	"fmt"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"
)

func GenerateSlurmConfig(cluster *values.SlurmCluster) (ConfFile, error) {
	res := propertiesConfig{}

	res.addProperty("ClusterName", cluster.Name)
	res.addComment("")
	// example: SlurmctldHost=controller-0(controller-0.controller.slurm-poc.svc.cluster.local)
	for i := range cluster.NodeController.Size {
		instanceName, instanceFQDN := naming.BuildServiceFQDN(
			consts.ComponentTypeController,
			cluster.Namespace,
			cluster.Name,
			cluster.NodeController.Service.Name,
			i,
		)
		res.addProperty("SlurmctldHost", fmt.Sprintf("%s(%s)", instanceName, instanceFQDN))
	}
	res.addComment("")
	res.addProperty("AuthType", "auth/slurm")
	res.addProperty("CredType", "cred/slurm")
	res.addComment("")
	res.addProperty("GresTypes", "gpu")
	res.addProperty("MailProg", "/usr/bin/true")
	res.addProperty("PluginDir", "/usr/local/lib/slurm")
	res.addProperty("ProctrackType", "proctrack/linuxproc")
	res.addProperty("ReturnToService", 1)
	res.addComment("")
	res.addProperty("SlurmctldPidFile", "/var/run/slurmctld.pid")
	res.addProperty("SlurmctldPort", cluster.NodeController.Service.Port)
	res.addComment("")
	res.addProperty("SlurmdPidFile", "/var/run/slurmd.pid")
	res.addProperty("SlurmdPort", cluster.NodeController.Service.Port) // FIXME this must be worker service port
	res.addComment("")
	{
		slurmDSpoolDir, err := naming.BuildVolumeMountSpoolPath(consts.ComponentTypeWorker)
		if err != nil {
			return nil, err
		}
		res.addProperty("SlurmdSpoolDir", slurmDSpoolDir)
	}
	res.addComment("")
	res.addProperty("SlurmUser", "root")
	res.addComment("")
	{
		slurmCtrlDSpoolDir, err := naming.BuildVolumeMountSpoolPath(consts.ComponentTypeController)
		if err != nil {
			return nil, err
		}
		res.addProperty("StateSaveLocation", slurmCtrlDSpoolDir)
	}
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
	res.addProperty("SlurmctldLogFile", "/var/log/slurmctld.log")
	res.addProperty("SlurmdDebug", "debug3")
	res.addProperty("SlurmdLogFile", "/var/log/slurmd.log")
	res.addComment("")
	res.addComment("COMPUTE NODES")
	res.addComment("We're using the \"dynamic nodes\" feature: https://slurm.schedmd.com/dynamic_nodes.html")
	res.addProperty("MaxNodeCount", "512")
	res.addProperty("PartitionName", "main Nodes=ALL Default=YES MaxTime=INFINITE State=UP")

	return res, nil
}
