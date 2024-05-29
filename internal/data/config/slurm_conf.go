package config

import (
	"fmt"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/models/k8s"
	"nebius.ai/slurm-operator/internal/models/k8s/naming"
)

func GenerateSlurmConfig(svc k8smodels.Service, clusterName string) ConfFile {
	res := propertiesConfig{}

	res.addProperty("ClusterName", clusterName)
	res.addProperty("SlurmctldHost", fmt.Sprintf("%s(%s)", svc.Name, k8snaming.BuildServiceFQDN(svc.Namespace, clusterName, consts.ComponentTypeController)))
	res.addProperty("AuthType", "auth/slurm")
	res.addProperty("CredType", "cred/slurm")
	res.addProperty("MailProg", "/usr/bin/true")
	res.addProperty("PluginDir", "/usr/local/lib/slurm")
	res.addProperty("ProctrackType", "proctrack/linuxproc")
	res.addProperty("ReturnToService", 1)
	res.addProperty("SlurmctldPidFile", "/var/run/slurmctld.pid")
	res.addProperty("SlurmctldPort", consts.ServiceControllerClusterPort)
	res.addProperty("SlurmdPidFile", "/var/run/slurmd.pid")
	res.addProperty("SlurmdPort", consts.ServiceWorkerClusterPort)
	res.addProperty("SlurmdSpoolDir", "/var/spool/slurmd")
	res.addProperty("SlurmUser", "root")
	res.addProperty("StateSaveLocation", "/var/spool/slurmctld")
	res.addProperty("TaskPlugin", "task/affinity")
	res.addProperty("InactiveLimit", 0)
	res.addProperty("KillWait", 30)
	res.addProperty("MinJobAge", 300)
	res.addProperty("SlurmctldTimeout", 120)
	res.addProperty("SlurmdTimeout", 300)
	res.addProperty("Waittime", 0)
	res.addProperty("SchedulerType", "sched/backfill")
	res.addProperty("SelectType", "select/cons_tres")
	res.addProperty("JobCompType", "jobcomp/none")
	res.addProperty("JobAcctGatherFrequency", 30)
	res.addProperty("SlurmctldDebug", "info")
	res.addProperty("SlurmctldLogFile", "/var/log/slurmctld.log")
	res.addProperty("SlurmdDebug", "info")
	res.addProperty("SlurmdLogFile", "/var/log/slurmd.log")
	res.addProperty("NodeName", "worker-0 NodeHostname=worker-0 NodeAddr=worker-0.slurm-poc.svc.cluster.local CPUs=56 Boards=1 SocketsPerBoard=1 CoresPerSocket=28 ThreadsPerCore=2 State=UNKNOWN")
	res.addProperty("PartitionName", "debug Nodes=ALL Default=YES MaxTime=INFINITE State=UP")

	return res
}
