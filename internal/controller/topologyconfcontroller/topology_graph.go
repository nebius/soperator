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

// tierZeroKey is the label naming the widest network domain a node belongs to: the unit a block
// topology groups by, and the switch closest to the root of a tree topology when the node carries
// it.
const tierZeroKey = "tier-0"

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

// RenderSwitches flattens the graph into the switch entries of a tree topology, in the shape
// topology.yaml expects. Only SWITCH vertices are emitted; worker leaves appear in their parent's
// node list.
func (g TopologyGraph) RenderSwitches() []switchYAML {
	var switches []switchYAML
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

		entry := switchYAML{Switch: slurmSafeSwitchName(parent)}
		if hasGrandChildren {
			// Children are switches: sanitize each so Slurm's hostlist parser cannot overflow a
			// long trailing decimal run and break the parent/child reference.
			safeChildren := make([]string, len(children))
			for i, child := range children {
				safeChildren[i] = slurmSafeSwitchName(child)
			}
			entry.Children = slurmpattern.Merge(safeChildren)
		} else {
			// Children are worker nodes, collapsed into a hostlist expression the same way blocks
			// are. Their names are not sanitized: unlike switch names they must expand back to the
			// real Slurm node names.
			entry.Nodes = slurmpattern.Merge(children)
		}
		switches = append(switches, entry)
	}
	slices.SortFunc(switches, func(a, b switchYAML) int {
		return strings.Compare(a.Switch, b.Switch)
	})
	return switches
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

		for _, worker := range workers {
			graph.AddEdge(pathToRoot[0], worker)
			placed[worker] = struct{}{}
			addTopSwitch(fabricOf(fabricByNode, worker), pathToRoot[len(pathToRoot)-1])
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
	graph.attachDirectNodeLeaves()

	return graph
}

// attachDirectNodeLeaves moves workers off switches that also have child switches. Slurm ignores
// children when a switch has a node list, so direct workers need a separate leaf in that case.
func (g TopologyGraph) attachDirectNodeLeaves() {
	var parents []string
	occupied := make(map[string]struct{})
	for parent, children := range g.children {
		parents = append(parents, parent)
		occupied[slurmSafeSwitchName(parent)] = struct{}{}
		for child := range children {
			occupied[slurmSafeSwitchName(child)] = struct{}{}
		}
	}
	slices.Sort(parents)

	for _, parent := range parents {
		var workers []string
		hasSwitches := false
		for child := range g.children[parent] {
			if len(g.children[child]) > 0 {
				hasSwitches = true
			} else {
				workers = append(workers, child)
			}
		}
		if !hasSwitches || len(workers) == 0 {
			continue
		}

		base := slurmSafeSwitchName(parent)
		suffix := ".nodes"
		var leaf string
		for attempt := 0; ; attempt++ {
			leaf = base[:min(len(base), maxSwitchNameLength-len(suffix))] + suffix
			if _, exists := occupied[leaf]; !exists {
				break
			}
			suffix = ".nodes" + strconv.Itoa(attempt+1)
		}
		occupied[leaf] = struct{}{}
		g.AddEdge(parent, leaf)
		for _, worker := range workers {
			delete(g.children[parent], worker)
			g.AddEdge(leaf, worker)
		}
	}
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

// labelsToPath converts labels to a path from the node to the root of the topology tree.
//
// Tiers are numbered from the root down: "tier-0", when the node carries it, is the switch closest
// to the fabric root, "tier-1" sits below it, and so on. The node itself hangs off the highest tier
// it is labelled with, so the deeper the label set, the deeper the node sits.
//
//	labels = map[string]string{"tier-1": "switch1", "tier-2": "switch2", "tier-3": "switch3"}
//	returns ["switch3", "switch2", "switch1"] (from the node up to the root)
//
//	labels = map[string]string{"tier-0": "spine1", "tier-1": "switch1"}
//	returns ["switch1", "spine1"]
//
// tier-0 is optional: without it the chain simply starts at tier-1, which then becomes the switch
// closest to the root. A node carrying tier-0 alone hangs off it directly.
//
// The tiers above 0 must be in the format "tier-N" where N is a positive integer starting from 1,
// and must form a contiguous chain: if any of them is missing (or empty), it returns an error.
func labelsToPath(labels map[string]string) ([]string, error) {
	numOfTiers := 0
	for key := range labels {
		if key != tierZeroKey && strings.HasPrefix(key, "tier-") {
			numOfTiers++
		}
	}

	tierZero := labels[tierZeroKey]
	if numOfTiers == 0 && tierZero == "" {
		return nil, fmt.Errorf("no labels found for node")
	}

	pathToRoot := make([]string, 0, numOfTiers+1)
	onPath := make(map[string]struct{}, numOfTiers+1)
	// A fabric with fewer real levels than the node has labels names the same switch at several
	// tiers. Keeping the repeat would make that switch its own ancestor: adjacent repeats give a
	// self-edge, distant ones a cycle, and either way nothing on that path reaches the fabric
	// root. A name already on the path is therefore skipped rather than linked again.
	appendTier := func(name string) {
		if _, ok := onPath[name]; ok {
			return
		}
		onPath[name] = struct{}{}
		pathToRoot = append(pathToRoot, name)
	}

	for i := numOfTiers; i >= 1; i-- {
		key := "tier-" + strconv.Itoa(i)
		curTierLabel := labels[key]
		if curTierLabel == "" {
			return nil, fmt.Errorf("missing label %q", key)
		}
		appendTier(curTierLabel)
	}
	if tierZero != "" {
		appendTier(tierZero)
	}
	return pathToRoot, nil
}
