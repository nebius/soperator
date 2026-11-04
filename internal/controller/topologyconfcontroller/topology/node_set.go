package topology

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultFabric is the fabric of NodeSets that configure none. A fabric names its root switch.
const DefaultFabric = "root"

// NodeSet is a range of Slurm nodes with the same name prefix. Built with NewNodeSet, so the
// paths it carries belong to its nodes.
type NodeSet struct {
	name             string
	size             uint
	fabric           string
	nodeNetworkPaths map[string]NetworkPath
}

// NewNodeSet validates and creates a NodeSet. An empty fabric means DefaultFabric.
// nodeNetworkPaths maps node name to path for the nodes that have one; a node absent from it
// (e.g. a down one) sits under the fabric's unknown leaf or block. A path of a foreign node is an error.
func NewNodeSet(name string, size uint, fabric string, nodeNetworkPaths map[string]NetworkPath) (NodeSet, error) {
	if fabric == "" {
		fabric = DefaultFabric
	}
	nodeSet := NodeSet{name: name, size: size, fabric: fabric, nodeNetworkPaths: nodeNetworkPaths}
	for node := range nodeNetworkPaths {
		if !nodeSet.hasNode(node) {
			return NodeSet{}, fmt.Errorf("validate NodeSet %q: node %q of its network paths is not one of its nodes", name, node)
		}
	}
	return nodeSet, nil
}

func (ns NodeSet) Name() string {
	return ns.name
}

// hasNode reports whether the node name belongs to this nodeset.
func (ns NodeSet) hasNode(name string) bool {
	suffix, ok := strings.CutPrefix(name, ns.name+"-")
	if !ok {
		return false
	}
	index, err := strconv.ParseUint(suffix, 10, 64)
	return err == nil && strconv.FormatUint(index, 10) == suffix && index < uint64(ns.size)
}

// node is a Slurm node with what the model needs from it.
type node struct {
	name    string
	nodeSet string
	fabric  string
	path    []NetworkPathSegment
}

// nodes returns every node of the NodeSet, "<name>-0" to "<name>-<size-1>".
func (ns NodeSet) nodes() []node {
	nodes := make([]node, 0, ns.size)
	for i := range ns.size {
		name := fmt.Sprintf("%s-%d", ns.name, i)
		nodes = append(nodes, node{name: name, nodeSet: ns.name, fabric: ns.fabric, path: ns.nodeNetworkPaths[name].segments})
	}
	return nodes
}
