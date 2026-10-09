package common

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"

	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func generateNamespaceConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine("defaults:")
	res.AddLine("  auto_base_path: true")
	res.AddLine("  base_path: /var/spool/slurmd/job-container/%n")
	res.AddLine("  shared: true")
	addNamespaceDirectories(res, "  ", nil, nil)

	var addedNodeConfigs bool
	for _, nodeSet := range cluster.NodeSets {
		if nodeSet.Spec.Replicas == 0 {
			continue
		}
		if !addedNodeConfigs {
			res.AddLine("node_confs:")
			addedNodeConfigs = true
		}
		nodeRange := nodeSet.Name + "-0"
		if nodeSet.Spec.Replicas > 1 {
			nodeRange = fmt.Sprintf("%s-[0-%d]", nodeSet.Name, nodeSet.Spec.Replicas-1)
		}
		// Slurm replaces the full dir_confs list for a matching node configuration.
		// Keep one hostlist per NodeSet so namespace config does not grow per worker.
		res.AddLine(fmt.Sprintf("  - nodes: [%q]", nodeRange))
		res.AddLine("    options:")
		addNamespaceDirectories(res, "      ", nodeSet.Spec.Slurmd.Resources.Memory(), nodeSet.Spec.Slurmd.Volumes.SharedMemorySize)
	}
	return res
}

func addNamespaceDirectories(res *renderutils.MultilineStringConfig, indent string, memory, sharedMemory *resource.Quantity) {
	res.AddLine(indent + "dir_confs:")
	for _, dir := range []struct {
		path string
		size *resource.Quantity
	}{
		{"/mnt/memory", memory},
		{"/dev/shm", sharedMemory},
		{"/tmp", memory},
	} {
		options := "mode=1777"
		if dir.size != nil && dir.size.Sign() > 0 {
			options += fmt.Sprintf(",size=%d", dir.size.Value())
		}
		res.AddLine(fmt.Sprintf("%s  - path: %s", indent, dir.path))
		res.AddLine(indent + "    tmpfs: true")
		res.AddLine(fmt.Sprintf("%s    options: %q", indent, options))
	}
}
