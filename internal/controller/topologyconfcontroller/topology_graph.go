package topologyconfcontroller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TopologyGraph is a topology forest, i.e. a disjoint set of trees.
// Leaves of the trees represent SLURM nodes.
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

// ensureSingleRoot adds all parentless switches as children of a single "root" switch
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

	// If there are multiple parentless switches, add them under "root"
	if len(rootChildren) > 1 {
		for _, child := range rootChildren {
			g.AddEdge("root", child)
		}
	}
}

// RenderConfigLines renders the topology graph as a list of configuration lines.
// Each line represents a vertex in the graph, with list of its children.
// The format is:
// SwitchName=<switch_name> Switches=<child1,child2,...>
// or
// SwitchName=<switch_name> Nodes=<child1,child2,...>
// where <switch_name> is the name of the vertex, and <child1,child2,...> is a comma-separated list of its children.
// If a vertex has no children, it is skipped (i.e. leaves of the tree are not rendered).
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
			lines = append(lines, fmt.Sprintf("SwitchName=%s Switches=%s", parent, strings.Join(children, ",")))
		} else {
			lines = append(lines, fmt.Sprintf("SwitchName=%s Nodes=%s", parent, strings.Join(children, ",")))
		}
	}
	slices.Sort(lines)
	return lines
}

func BuildTopologyGraph(
	ctx context.Context, labelsByNode map[string]NodeTopologyLabels, podsByNode map[string][]string,
) TopologyGraph {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)
	graph := newTopologyGraph()
	podsByNode = maps.Clone(podsByNode)
	for node, labels := range labelsByNode {
		pathToRoot, err := labelsToPath(labels)
		if err != nil {
			logger.Error(err, "Invalid node topology labels", "node", node, "labels", labels)
			continue
		}

		workers := podsByNode[node]
		delete(podsByNode, node)

		for _, worker := range workers {
			graph.AddEdge(pathToRoot[0], worker)
		}
		for i := range len(pathToRoot) - 1 {
			graph.AddEdge(pathToRoot[i+1], pathToRoot[i])
		}
	}

	// Add rest of the pods for unknown nodes.
	const unknownSwitchName = "unknown"
	for _, pods := range podsByNode {
		for _, worker := range pods {
			graph.AddEdge(unknownSwitchName, worker)
		}
	}

	// Ensure all top-level switches are under a single root
	graph.ensureSingleRoot()

	return graph
}

// labelsToPath converts labels to a path to the root of the topology tree.
// E.g.:
//
//	labels = map[string]string{"tier-1": "switch1", "tier-2": "switch2", "tier-3": "switch3"}
//	returns ["switch3", "switch2", "switch1"]
//
// The labels must be in the format "tier-N" where N is a positive integer starting from 1.
// If any label is missing (or empty), it returns an error.
func labelsToPath(labels map[string]string) ([]string, error) {
	if len(labels) == 0 {
		return nil, fmt.Errorf("no labels found for node")
	}
	pathToRoot := make([]string, 0, len(labels))
	for i := range len(labels) {
		key := "tier-" + strconv.Itoa(i+1)
		curTierLabel := labels[key]
		if curTierLabel == "" {
			return nil, fmt.Errorf("missing label %q", key)
		}
		pathToRoot = append(pathToRoot, curTierLabel)
	}
	return pathToRoot, nil
}
