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

type NCCLType string

const (
	NCCLTypeAuto           NCCLType = "auto"
	NCCLTypeH100GPUCluster NCCLType = "H100 GPU cluster"
	NCCLTypeCustom         NCCLType = "custom"
)

// RenderConfigMapNCCLTopology renders new [corev1.ConfigMap] containing NCCL topology config file
func RenderConfigMapNCCLTopology(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	topology, err := generateVirtualTopology(
		NCCLType(cluster.NodeWorker.NCCLSettings.TopologyType),
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

func generateVirtualTopology(ncclType NCCLType, topologyData string) (renderutils.ConfigFile, error) {
	res := &renderutils.RawConfig{}
	switch ncclType {
	case NCCLTypeAuto:
		return res, nil
	case NCCLTypeH100GPUCluster:
		return generateVirtualH100GPUClusterTopology(), nil
	case NCCLTypeCustom:
		if topologyData != "" {
			return renderutils.NewAsIsConfig(topologyData), nil
		}
		return res, errors.New("topologyData can't be empty for custom type of NCCL topology")
	default:
		return res, nil
	}
}

func generateVirtualH100GPUClusterTopology() renderutils.ConfigFile {
	res := &renderutils.RawConfig{}
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
