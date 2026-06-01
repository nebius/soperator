package topologyconfcontroller

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

// TopologyGraph represents a network topology as a single tree with two types of vertices:
//
// 1. SWITCHES: Infrastructure nodes (spine, leaf, core switches) that represent network hierarchy.
//   - Always have children (either other switches or worker nodes)
//   - Are rendered as "SwitchName=X Switches=..." or "SwitchName=X Nodes=..." lines
//   - Form the hierarchical backbone of the network topology
//
// 2. WORKERS: Compute nodes that execute Slurm jobs.
//   - Have no children (leaf nodes in the tree)
//   - Are NOT rendered as separate configuration lines
//   - Only appear in "Nodes=" lists of their parent switches
//
// The graph maintains a single tree structure (using artificial "root" if needed) to ensure
// strong connectivity - this is required for Slurm to schedule jobs across all nodes.
type TopologyGraph struct {
	// children[vertex] is set of children of a vertex.
	children map[string]map[string]struct{}
}

func newTopologyGraph() TopologyGraph {
	return TopologyGraph{
		children: make(map[string]map[string]struct{}),
	}
}

func (g TopologyGraph) AddEdge(parent, child string) {
	if _, ok := g.children[parent]; !ok {
		g.children[parent] = make(map[string]struct{})
	}
	g.children[parent][child] = struct{}{}
}

// ensureSingleRoot ensures the topology forms a single tree by adding all parentless switches
// as children of a single "root" switch. This is required for Slurm's strong connectivity
// requirement - all nodes must be reachable from each other for job scheduling to work.
func (g TopologyGraph) ensureSingleRoot() {
	// Find all nodes that have parents
	hasParent := make(map[string]bool)
	for _, children := range g.children {
		for child := range children {
			hasParent[child] = true
		}
	}

	// Collect all parentless switches (except "root" itself)
	var rootChildren []string
	for switch_ := range g.children {
		if !hasParent[switch_] && switch_ != "root" {
			rootChildren = append(rootChildren, switch_)
		}
	}

	// Always connect top-level switches to a synthetic "root" switch.
	// This guarantees a stable single-root tree in topology.conf.
	if len(rootChildren) > 0 {
		// Sort children for consistent output
		slices.Sort(rootChildren)
		for _, child := range rootChildren {
			g.AddEdge("root", child)
		}
	}
}

// RenderConfigLines renders only SWITCH vertices as Slurm topology configuration lines.
// WORKER vertices (leaves) are not rendered as separate lines - they only appear in
// "Nodes=" lists of their parent switches.
//
// The format is:
//
//	SwitchName=<switch_name> Switches=<child1,child2,...>  (if children are switches)
//	SwitchName=<switch_name> Nodes=<child1,child2,...>     (if children are workers)
//
// This distinction is critical: switches with grandchildren use "Switches=",
// while switches with only worker children use "Nodes=".
func (g TopologyGraph) RenderConfigLines() []string {
	var lines []string
	for parent, childrenSet := range g.children {
		if len(childrenSet) == 0 {
			continue // Skip leaves (worker nodes).
		}
		hasGrandChildren := false
		children := make([]string, 0, len(childrenSet))
		for child := range childrenSet {
			if len(g.children[child]) > 0 {
				hasGrandChildren = true
			}
			children = append(children, child)
		}
		slices.Sort(children)
		if hasGrandChildren {
			lines = append(lines, fmt.Sprintf("SwitchName=%s Switches=%s", parent, slurmpattern.Merge(children)))
		} else {
			lines = append(lines, fmt.Sprintf("SwitchName=%s Nodes=%s", parent, strings.Join(children, ",")))
		}
	}
	slices.Sort(lines)
	return lines
}

// BuildTopologyGraph constructs a single tree topology in two stages.
//
// Stage 1 places every Slurm node from allNodeNames under the synthetic "unknown" switch, so the
// topology stays complete and stable regardless of pod lifecycle (powered-down ephemeral nodes
// included). Stage 2 overlays IB switches: GPU pods that are scheduled to a labeled K8s node
// (gpuPodsByNode) are moved from "unknown" onto their real switch path. Non-GPU nodes and
// unscheduled or unlabeled GPU nodes stay under "unknown".
//
// The tree construction ensures strong connectivity required for Slurm job scheduling.
func BuildTopologyGraph(
	ctx context.Context,
	labelsByNode map[string]NodeTopologyLabels,
	gpuPodsByNode map[string][]string,
	allNodeNames []string,
) TopologyGraph {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)
	graph := newTopologyGraph()

	// Stage 2: place scheduled GPU pods onto their IB switch path.
	placed := make(map[string]struct{})
	for node, labels := range labelsByNode {
		workers := gpuPodsByNode[node]
		if len(workers) == 0 {
			continue
		}

		pathToRoot, err := labelsToPath(labels)
		if err != nil {
			// Pods fall back to the "unknown" switch via stage 1.
			logger.Error(err, "Invalid node topology labels", "node", node, "labels", labels)
			continue
		}

		for _, worker := range workers {
			graph.AddEdge(pathToRoot[0], worker)
			placed[worker] = struct{}{}
		}
		for i := range len(pathToRoot) - 1 {
			graph.AddEdge(pathToRoot[i+1], pathToRoot[i])
		}
	}

	// Stage 1: every node not placed on a real switch goes under "unknown".
	const unknownSwitchName = "unknown"
	for _, name := range allNodeNames {
		if _, ok := placed[name]; ok {
			continue
		}
		graph.AddEdge(unknownSwitchName, name)
	}

	// Ensure all top-level switches are under a single root
	graph.ensureSingleRoot()

	return graph
}

// labelsToPath converts labels to a path to the root of the topology tree.
// E.g.:
//
//	labels = map[string]string{"tier-1": "switch1", "tier-2": "switch2", "tier-3": "switch3"}
//	returns ["switch1", "switch2", "switch3"] (from lowest to highest tier)
//
// The labels must be in the format "tier-N" where N is a positive integer starting from 1.
// If any label is missing (or empty), it returns an error.
// In case of "tier-0" label provided - we ignore it and check only remaining "tier-N" labels.
// ("tier-0" used for defining block, not IB topology)
func labelsToPath(labels map[string]string) ([]string, error) {
	if len(labels) == 0 {
		return nil, fmt.Errorf("no labels found for node")
	}
	pathToRoot := make([]string, 0, len(labels))

	numOfTiers := len(labels)
	if _, hasTierZero := labels["tier-0"]; hasTierZero {
		numOfTiers--
	}
	for i := range numOfTiers {
		key := "tier-" + strconv.Itoa(i+1)
		curTierLabel := labels[key]
		if curTierLabel == "" {
			return nil, fmt.Errorf("missing label %q", key)
		}
		pathToRoot = append(pathToRoot, curTierLabel)
	}
	return pathToRoot, nil
}
