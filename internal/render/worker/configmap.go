package worker

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// region NCCL topology

// RenderConfigMapNCCLTopology renders new [corev1.ConfigMap] containing NCCL topology config file
func RenderConfigMapNCCLTopology(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	ncclType, err := consts.StringToNCCLType(cluster.NodeWorker.NCCLSettings.TopologyType)
	if err != nil {
		return corev1.ConfigMap{}, err
	}
	topology, err := generateVirtualTopology(
		ncclType,
		cluster.NodeWorker.NCCLSettings.TopologyData,
	)
	if err != nil {
		return corev1.ConfigMap{}, err
	}

	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapNCCLTopologyName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeyNCCLTopology: topology.Render(),
		},
	}, nil
}

func generateVirtualTopology(ncclType consts.NCCLType, topologyData string) (renderutils.ConfigFile, error) {
	res := &renderutils.MultilineStringConfig{}
	switch ncclType {
	case consts.NCCLTypeAuto:
		return res, nil
	case consts.NCCLTypeH100GPUCluster:
		return generateVirtualH100GPUClusterTopology(), nil
	case consts.NCCLTypeCustom:
		if topologyData != "" {
			return renderutils.NewAsIsConfig(topologyData), nil
		}
		return res, errors.New("topologyData can't be empty for custom type of NCCL topology")
	default:
		return res, nil
	}
}

func generateVirtualH100GPUClusterTopology() renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine("<system version=\"1\">")
	res.AddLine("    <cpu numaid=\"0\" affinity=\"00000000,00000000,0000ffff,ffffffff,ffffffff\" arch=\"x86_64\" vendor=\"GenuineIntel\" familyid=\"6\" modelid=\"106\">")
	res.AddLine("        <pci busid=\"0000:8a:00.0\" class=\"0x060400\" vendor=\"0x104c\" device=\"0x8232\" subsystem_vendor=\"0x0000\" subsystem_device=\"0x0000\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("            <pci busid=\"0000:8c:00.0\" class=\"0x020700\" vendor=\"0x15b3\" device=\"0x101e\" subsystem_vendor=\"0x15b3\" subsystem_device=\"0x0023\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("                <nic>")
	res.AddLine("                    <net name=\"mlx5_4\" dev=\"4\" speed=\"400000\" port=\"1\" latency=\"0.000000\" maxconn=\"131072\" gdr=\"1\" coll=\"1\"/>")
	res.AddLine("                </nic>")
	res.AddLine("            </pci>")
	res.AddLine("            <pci busid=\"0000:8d:00.0\" class=\"0x030200\" vendor=\"0x10de\" device=\"0x2330\" subsystem_vendor=\"0x10de\" subsystem_device=\"0x16c1\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\"/>")
	res.AddLine("        </pci>")
	res.AddLine("        <pci busid=\"0000:8e:00.0\" class=\"0x060400\" vendor=\"0x104c\" device=\"0x8232\" subsystem_vendor=\"0x0000\" subsystem_device=\"0x0000\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("            <pci busid=\"0000:90:00.0\" class=\"0x020700\" vendor=\"0x15b3\" device=\"0x101e\" subsystem_vendor=\"0x15b3\" subsystem_device=\"0x0023\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("                <nic>")
	res.AddLine("                    <net name=\"mlx5_5\" dev=\"5\" speed=\"400000\" port=\"1\" latency=\"0.000000\" maxconn=\"131072\" gdr=\"1\" coll=\"1\"/>")
	res.AddLine("                </nic>")
	res.AddLine("            </pci>")
	res.AddLine("            <pci busid=\"0000:91:00.0\" class=\"0x030200\" vendor=\"0x10de\" device=\"0x2330\" subsystem_vendor=\"0x10de\" subsystem_device=\"0x16c1\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\"/>")
	res.AddLine("        </pci>")
	res.AddLine("        <pci busid=\"0000:92:00.0\" class=\"0x060400\" vendor=\"0x104c\" device=\"0x8232\" subsystem_vendor=\"0x0000\" subsystem_device=\"0x0000\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("            <pci busid=\"0000:94:00.0\" class=\"0x020700\" vendor=\"0x15b3\" device=\"0x101e\" subsystem_vendor=\"0x15b3\" subsystem_device=\"0x0023\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("                <nic>")
	res.AddLine("                    <net name=\"mlx5_6\" dev=\"6\" speed=\"400000\" port=\"1\" latency=\"0.000000\" maxconn=\"131072\" gdr=\"1\" coll=\"1\"/>")
	res.AddLine("                </nic>")
	res.AddLine("            </pci>")
	res.AddLine("            <pci busid=\"0000:95:00.0\" class=\"0x030200\" vendor=\"0x10de\" device=\"0x2330\" subsystem_vendor=\"0x10de\" subsystem_device=\"0x16c1\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\"/>")
	res.AddLine("        </pci>")
	res.AddLine("        <pci busid=\"0000:96:00.0\" class=\"0x060400\" vendor=\"0x104c\" device=\"0x8232\" subsystem_vendor=\"0x0000\" subsystem_device=\"0x0000\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("            <pci busid=\"0000:98:00.0\" class=\"0x020700\" vendor=\"0x15b3\" device=\"0x101e\" subsystem_vendor=\"0x15b3\" subsystem_device=\"0x0023\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("                <nic>")
	res.AddLine("                    <net name=\"mlx5_7\" dev=\"7\" speed=\"400000\" port=\"1\" latency=\"0.000000\" maxconn=\"131072\" gdr=\"1\" coll=\"1\"/>")
	res.AddLine("                </nic>")
	res.AddLine("            </pci>")
	res.AddLine("            <pci busid=\"0000:99:00.0\" class=\"0x030200\" vendor=\"0x10de\" device=\"0x2330\" subsystem_vendor=\"0x10de\" subsystem_device=\"0x16c1\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\"/>")
	res.AddLine("        </pci>")
	res.AddLine("    </cpu>")
	res.AddLine("    <cpu numaid=\"1\" affinity=\"ffffffff,ffffffff,ffff0000,00000000,00000000\" arch=\"x86_64\" vendor=\"GenuineIntel\" familyid=\"6\" modelid=\"106\">")
	res.AddLine("        <pci busid=\"0000:a8:00.0\" class=\"0x060400\" vendor=\"0x104c\" device=\"0x8232\" subsystem_vendor=\"0x0000\" subsystem_device=\"0x0000\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("            <pci busid=\"0000:aa:00.0\" class=\"0x020700\" vendor=\"0x15b3\" device=\"0x101e\" subsystem_vendor=\"0x15b3\" subsystem_device=\"0x0023\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("                <nic>")
	res.AddLine("                    <net name=\"mlx5_0\" dev=\"0\" speed=\"400000\" port=\"1\" latency=\"0.000000\" maxconn=\"131072\" gdr=\"1\" coll=\"1\"/>")
	res.AddLine("                </nic>")
	res.AddLine("            </pci>")
	res.AddLine("            <pci busid=\"0000:ab:00.0\" class=\"0x030200\" vendor=\"0x10de\" device=\"0x2330\" subsystem_vendor=\"0x10de\" subsystem_device=\"0x16c1\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\"/>")
	res.AddLine("        </pci>")
	res.AddLine("        <pci busid=\"0000:ac:00.0\" class=\"0x060400\" vendor=\"0x104c\" device=\"0x8232\" subsystem_vendor=\"0x0000\" subsystem_device=\"0x0000\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("            <pci busid=\"0000:ae:00.0\" class=\"0x020700\" vendor=\"0x15b3\" device=\"0x101e\" subsystem_vendor=\"0x15b3\" subsystem_device=\"0x0023\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("                <nic>")
	res.AddLine("                    <net name=\"mlx5_1\" dev=\"1\" speed=\"400000\" port=\"1\" latency=\"0.000000\" maxconn=\"131072\" gdr=\"1\" coll=\"1\"/>")
	res.AddLine("                </nic>")
	res.AddLine("            </pci>")
	res.AddLine("            <pci busid=\"0000:af:00.0\" class=\"0x030200\" vendor=\"0x10de\" device=\"0x2330\" subsystem_vendor=\"0x10de\" subsystem_device=\"0x16c1\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\"/>")
	res.AddLine("        </pci>")
	res.AddLine("        <pci busid=\"0000:b0:00.0\" class=\"0x060400\" vendor=\"0x104c\" device=\"0x8232\" subsystem_vendor=\"0x0000\" subsystem_device=\"0x0000\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("            <pci busid=\"0000:b2:00.0\" class=\"0x020700\" vendor=\"0x15b3\" device=\"0x101e\" subsystem_vendor=\"0x15b3\" subsystem_device=\"0x0023\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("                <nic>")
	res.AddLine("                    <net name=\"mlx5_2\" dev=\"2\" speed=\"400000\" port=\"1\" latency=\"0.000000\" maxconn=\"131072\" gdr=\"1\" coll=\"1\"/>")
	res.AddLine("                </nic>")
	res.AddLine("            </pci>")
	res.AddLine("            <pci busid=\"0000:b3:00.0\" class=\"0x030200\" vendor=\"0x10de\" device=\"0x2330\" subsystem_vendor=\"0x10de\" subsystem_device=\"0x16c1\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\"/>")
	res.AddLine("        </pci>")
	res.AddLine("        <pci busid=\"0000:b4:00.0\" class=\"0x060400\" vendor=\"0x104c\" device=\"0x8232\" subsystem_vendor=\"0x0000\" subsystem_device=\"0x0000\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("            <pci busid=\"0000:b6:00.0\" class=\"0x020700\" vendor=\"0x15b3\" device=\"0x101e\" subsystem_vendor=\"0x15b3\" subsystem_device=\"0x0023\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\">")
	res.AddLine("                <nic>")
	res.AddLine("                    <net name=\"mlx5_3\" dev=\"3\" speed=\"400000\" port=\"1\" latency=\"0.000000\" maxconn=\"131072\" gdr=\"1\" coll=\"1\"/>")
	res.AddLine("                </nic>")
	res.AddLine("            </pci>")
	res.AddLine("            <pci busid=\"0000:b7:00.0\" class=\"0x030200\" vendor=\"0x10de\" device=\"0x2330\" subsystem_vendor=\"0x10de\" subsystem_device=\"0x16c1\" link_speed=\"32.0 GT/s PCIe\" link_width=\"16\"/>")
	res.AddLine("        </pci>")
	res.AddLine("    </cpu>")
	res.AddLine("</system>")
	return res
}

// endregion NCCL topology

// region Sysctl

// RenderConfigMapSysctl renders new [corev1.ConfigMap] containing sysctl config file
func RenderConfigMapSysctl(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSysctlName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySysctl: generateSysctlConfig().Render(),
		},
	}, nil
}

func generateSysctlConfig() renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	res.AddProperty("vm.max_map_count", 655300)
	return res
}

// endregion Sysctl

// region Supervisord

// RenderConfigMapSupervisord renders new [corev1.ConfigMap] containing supervisord config file
func RenderConfigMapSupervisord(cluster *values.SlurmCluster) corev1.ConfigMap {
	data := generateSupervisordConfig().Render()
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.NodeWorker.SupervisordConfigMapName,
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySupervisord: data,
		},
	}
}

func generateSupervisordConfig() renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine("[supervisord]")
	res.AddLine("nodaemon=true")
	res.AddLine("logfile=/dev/null ; Output only to stdout/stderr")
	res.AddLine("logfile_maxbytes=0")
	res.AddLine("pidfile=/var/run/supervisord.pid")
	res.AddLine("")
	res.AddLine("[program:slurmd]")
	res.AddLine("priority=1")
	res.AddLine("stdout_logfile=/dev/fd/1")
	res.AddLine("stdout_logfile_maxbytes=0")
	res.AddLine("stderr_logfile=/dev/fd/2")
	res.AddLine("stderr_logfile_maxbytes=0")
	res.AddLine("redirect_stderr=true")
	res.AddLine("command=/opt/bin/slurm/slurmd_entrypoint.sh")
	res.AddLine("autostart=true")
	res.AddLine("autorestart=true")
	res.AddLine("startretries=5")
	res.AddLine("stopasgroup=true ; Send SIGTERM to all child processes of supervisord")
	res.AddLine("killasgroup=true ; Send SIGKILL to all child processes of supervisord")
	res.AddLine("stopsignal=SIGTERM ; Signal to send to the program to stop it")
	res.AddLine("stopwaitsecs=10 ; Wait for the process to stop before sending a SIGKILL")
	res.AddLine("")
	res.AddLine("[program:sshd]")
	res.AddLine("priority=10")
	res.AddLine("stdout_logfile=/dev/fd/1")
	res.AddLine("stdout_logfile_maxbytes=0")
	res.AddLine("stderr_logfile=/dev/fd/2")
	res.AddLine("stderr_logfile_maxbytes=0")
	res.AddLine("redirect_stderr=true")
	res.AddLine("command=/usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config")
	res.AddLine("autostart=true")
	res.AddLine("autorestart=true")
	res.AddLine("startretries=5")
	res.AddLine("stopasgroup=true ; Send SIGTERM to all child processes of supervisord")
	res.AddLine("killasgroup=true ; Send SIGKILL to all child processes of supervisord")
	res.AddLine("stopsignal=SIGTERM ; Signal to send to the program to stop it")
	res.AddLine("stopwaitsecs=10 ; Wait for the process to stop before sending a SIGKILL")

	return res
}

// endregion Supervisord
