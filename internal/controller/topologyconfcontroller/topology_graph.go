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

// defaultFabric is the fabric name used for NodeSets that don't configure one. It keeps the
// legacy single-root behavior: root switch "root" with a child switch "unknown".
const defaultFabric = "root"

// fabricOf returns the fabric a Slurm node belongs to (from its NodeSet's spec.topology.fabric),
// defaulting to defaultFabric when the node has no explicit fabric.
func fabricOf(fabricByNode map[string]string, node string) string {
	if fabric := fabricByNode[node]; fabric != "" {
		return fabric
	}
	return defaultFabric
}

// unknownSwitchName returns the catch-all switch for nodes of the given fabric that have no usable
// IB topology labels. The default fabric keeps the legacy "unknown" name; named fabrics use
// "<fabric>.unknown".
func unknownSwitchName(fabric string) string {
	if fabric == defaultFabric {
		return "unknown"
	}
	return fabric + ".unknown"
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

// BuildTopologyGraph constructs the tree topology in two stages.
//
// Stage 1 places every Slurm node from allNodeNames under its fabric's "unknown" switch, so the
// topology stays complete and stable regardless of pod lifecycle (powered-down ephemeral nodes
// included). Stage 2 overlays IB switches: GPU pods that are scheduled to a labeled K8s node
// (gpuPodsByNode) are moved off "unknown" onto their real switch path. Non-GPU nodes and
// unscheduled or unlabeled GPU nodes stay under "unknown".
//
// Instead of a single synthetic "root", each IB fabric (from fabricByNode, keyed by Slurm node
// name and sourced from each NodeSet's spec.topology.fabric) gets its own root switch named after
// the fabric. These fabric roots stay unconnected, so Slurm never schedules a single job across
// fabrics. Nodes without an explicit fabric fall back to the default "root"/"unknown" naming,
// preserving the legacy single-fabric output.
func BuildTopologyGraph(
	ctx context.Context,
	labelsByNode map[string]NodeTopologyLabels,
	gpuPodsByNode map[string][]string,
	allNodeNames []string,
	fabricByNode map[string]string,
) TopologyGraph {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)
	graph := newTopologyGraph()

	// topSwitchesByFabric tracks, per fabric, the switches that top a node's IB path (or the
	// "unknown" switch). After all edges are built we attach those that turn out to be parentless
	// to their fabric root - mirroring the old single-root logic, but per fabric.
	topSwitchesByFabric := make(map[string]map[string]struct{})
	addTopSwitch := func(fabric, sw string) {
		if topSwitchesByFabric[fabric] == nil {
			topSwitchesByFabric[fabric] = make(map[string]struct{})
		}
		topSwitchesByFabric[fabric][sw] = struct{}{}
	}

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

		topSwitch := pathToRoot[len(pathToRoot)-1]
		for _, worker := range workers {
			graph.AddEdge(pathToRoot[0], worker)
			placed[worker] = struct{}{}
			addTopSwitch(fabricOf(fabricByNode, worker), topSwitch)
		}
		for i := range len(pathToRoot) - 1 {
			graph.AddEdge(pathToRoot[i+1], pathToRoot[i])
		}
	}

	// Stage 1: every node not placed on a real switch goes under its fabric's "unknown" switch.
	for _, name := range allNodeNames {
		if _, ok := placed[name]; ok {
			continue
		}
		fabric := fabricOf(fabricByNode, name)
		unknown := unknownSwitchName(fabric)
		graph.AddEdge(unknown, name)
		addTopSwitch(fabric, unknown)
	}

	graph.attachFabricRoots(topSwitchesByFabric)

	return graph
}

// attachFabricRoots connects each fabric's top switches to a root switch named after the fabric,
// but only those that are still parentless once the whole tree is built. A switch that tops a
// shallow node's path may be an intermediate switch in a deeper node's path (heterogeneous tier
// depths); such switches already have a parent and must not be re-parented to the fabric root.
func (g TopologyGraph) attachFabricRoots(topSwitchesByFabric map[string]map[string]struct{}) {
	hasParent := make(map[string]bool)
	for _, children := range g.children {
		for child := range children {
			hasParent[child] = true
		}
	}

	for fabric, switches := range topSwitchesByFabric {
		for sw := range switches {
			if !hasParent[sw] {
				g.AddEdge(fabric, sw)
			}
		}
	}
}

// labelsToPath converts labels to a path to the root of the topology tree.
// E.g.:
//
//	labels = map[string]string{"tier-1": "switch1", "tier-2": "switch2", "tier-3": "switch3"}
//	returns ["switch1", "switch2", "switch3"] (from lowest to highest tier)
//
// The labels must be in the format "tier-N" where N is a positive integer starting from 1.
// If any label is missing (or empty), it returns an error.
// Non-tier keys (e.g. "tier-0", used for defining a block) are ignored: only contiguous "tier-N"
// labels starting from 1 form the IB topology path.
func labelsToPath(labels map[string]string) ([]string, error) {
	numOfTiers := 0
	for key := range labels {
		if key != "tier-0" && strings.HasPrefix(key, "tier-") {
			numOfTiers++
		}
	}
	if numOfTiers == 0 {
		return nil, fmt.Errorf("no labels found for node")
	}

	pathToRoot := make([]string, 0, numOfTiers)
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
