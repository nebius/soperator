package sharedsteps

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	topologyKindTree  = "tree"
	topologyKindBlock = "block"
	topologyKindFlat  = "flat"
)

var (
	switchNamePattern     = regexp.MustCompile(`\bSwitchName=(\S+)`)
	switchLevelPattern    = regexp.MustCompile(`\bLevel=(\d+)`)
	switchChildrenPattern = regexp.MustCompile(`\bSwitches=(\S+)`)
	blockNamePattern      = regexp.MustCompile(`\bBlockName=(\S+)`)
	blockSizePattern      = regexp.MustCompile(`\bBlockSize=(\d+)`)
	nodesFieldPattern     = regexp.MustCompile(`\bNodes=(\S+)`)
	topologyFieldPattern  = regexp.MustCompile(`\bTopology=(\S+)`)
)

// topologyEntry is one entry of topology.yaml, reduced to what the acceptance checks read.
type topologyEntry struct {
	Name           string
	ClusterDefault bool
	Kind           string
	BlockSizes     []int
	Switches       []topologySwitch
	Blocks         []topologyUnit
}

// topologySwitch is one switch of a tree topology. Nodes stays the raw Slurm hostlist: expanding it
// costs a call to the controller, so it is done only where the node names are actually needed.
type topologySwitch struct {
	Name     string
	Children []string
	Nodes    string
}

type topologyUnit struct {
	Name  string
	Nodes string
}

// rawTopologyEntry decodes both sides of the comparison: the topology.yaml the operator renders and
// the same file as `scontrol show topoconf` prints it back. The plugin fields are yaml.Node because
// the two spellings differ - topoconf writes the plugins an entry does not use as empty maps
// ("tree: {}") where the rendered file simply omits them.
type rawTopologyEntry struct {
	Topology       string    `yaml:"topology"`
	ClusterDefault bool      `yaml:"cluster_default"`
	Tree           yaml.Node `yaml:"tree"`
	Block          yaml.Node `yaml:"block"`
	Flat           yaml.Node `yaml:"flat"`
}

type rawTreeBody struct {
	Switches []struct {
		Switch   string `yaml:"switch"`
		Children string `yaml:"children"`
		Nodes    string `yaml:"nodes"`
	} `yaml:"switches"`
}

type rawBlockBody struct {
	BlockSizes []int `yaml:"block_sizes"`
	Blocks     []struct {
		Block string `yaml:"block"`
		Nodes string `yaml:"nodes"`
	} `yaml:"blocks"`
}

// parseTopologyEntries decodes the body of topology.yaml. It accepts the managed-config warning
// header the operator writes and the "%YAML 1.1" document markers scontrol prints.
func parseTopologyEntries(raw string) ([]topologyEntry, error) {
	var rawEntries []rawTopologyEntry
	if err := yaml.Unmarshal([]byte(raw), &rawEntries); err != nil {
		return nil, fmt.Errorf("unmarshal topology.yaml: %w", err)
	}
	if len(rawEntries) == 0 {
		return nil, fmt.Errorf("topology.yaml declares no topology")
	}

	entries := make([]topologyEntry, 0, len(rawEntries))
	seen := make(map[string]struct{}, len(rawEntries))
	for i, rawEntry := range rawEntries {
		name := strings.TrimSpace(rawEntry.Topology)
		if name == "" {
			return nil, fmt.Errorf("topology.yaml entry %d has no name", i)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("topology %q is declared twice", name)
		}
		seen[name] = struct{}{}

		entry := topologyEntry{Name: name, ClusterDefault: rawEntry.ClusterDefault}
		var kinds []string

		if yamlNodeCarriesValue(&rawEntry.Tree) {
			kinds = append(kinds, topologyKindTree)
			var body rawTreeBody
			if err := rawEntry.Tree.Decode(&body); err != nil {
				return nil, fmt.Errorf("decode tree of topology %q: %w", name, err)
			}
			for _, sw := range body.Switches {
				entry.Switches = append(entry.Switches, topologySwitch{
					Name:     strings.TrimSpace(sw.Switch),
					Children: splitSlurmList(sw.Children),
					Nodes:    strings.TrimSpace(sw.Nodes),
				})
			}
		}
		if yamlNodeCarriesValue(&rawEntry.Block) {
			kinds = append(kinds, topologyKindBlock)
			var body rawBlockBody
			if err := rawEntry.Block.Decode(&body); err != nil {
				return nil, fmt.Errorf("decode block of topology %q: %w", name, err)
			}
			entry.BlockSizes = body.BlockSizes
			for _, block := range body.Blocks {
				entry.Blocks = append(entry.Blocks, topologyUnit{
					Name:  strings.TrimSpace(block.Block),
					Nodes: strings.TrimSpace(block.Nodes),
				})
			}
		}
		if yamlNodeCarriesValue(&rawEntry.Flat) {
			kinds = append(kinds, topologyKindFlat)
		}

		if len(kinds) != 1 {
			return nil, fmt.Errorf("topology %q declares %d plugins (%s), want exactly one",
				name, len(kinds), strings.Join(kinds, ", "))
		}
		entry.Kind = kinds[0]
		entries = append(entries, entry)
	}
	return entries, nil
}

// yamlNodeCarriesValue reports whether a plugin field of a topology.yaml entry selects that plugin.
// Presence of the key says nothing: scontrol spells unused plugins as empty maps, and the flat
// plugin is a plain boolean rather than a body.
func yamlNodeCarriesValue(node *yaml.Node) bool {
	switch {
	case node == nil, node.Kind == 0, node.Tag == "!!null":
		return false
	case node.Tag == "!!bool":
		return node.Value == "true"
	case node.Kind == yaml.MappingNode, node.Kind == yaml.SequenceNode:
		return len(node.Content) > 0
	default:
		return true
	}
}

// topologyStructure fingerprints what slurmctld can only learn by re-reading topology.yaml. It
// mirrors the operator's own fingerprint, so the rendered config and the loaded one can be compared
// without depending on node membership, which travels through the workers' own registrations.
func topologyStructure(entries []topologyEntry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, fmt.Sprintf("%s=%s:%v:%t",
			entry.Name, entry.Kind, entry.BlockSizes, entry.ClusterDefault))
	}
	return strings.Join(parts, ",")
}

func topologyByName(entries []topologyEntry, name string) (topologyEntry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return topologyEntry{}, false
}

func topologyNames(entries []topologyEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
}

// slurmSwitch is one line of `scontrol show topology <tree>`.
type slurmSwitch struct {
	Name     string
	Level    int
	Nodes    string
	Children []string
}

// IsLeaf reports whether the switch has workers attached directly. Slurm lists every descendant
// node on the ancestors too, so only level 0 identifies the switch a node actually hangs off.
func (s slurmSwitch) IsLeaf() bool {
	return s.Level == 0
}

func parseSlurmSwitches(out string) ([]slurmSwitch, error) {
	var switches []slurmSwitch
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		match := switchNamePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		sw := slurmSwitch{Name: match[1], Level: -1}
		if levelMatch := switchLevelPattern.FindStringSubmatch(line); levelMatch != nil {
			level, err := strconv.Atoi(levelMatch[1])
			if err != nil {
				return nil, fmt.Errorf("parse Level of switch %q: %w", sw.Name, err)
			}
			sw.Level = level
		}
		if sw.Level < 0 {
			return nil, fmt.Errorf("switch %q has no Level", sw.Name)
		}
		if nodesMatch := nodesFieldPattern.FindStringSubmatch(line); nodesMatch != nil {
			sw.Nodes = nodesMatch[1]
		}
		if childrenMatch := switchChildrenPattern.FindStringSubmatch(line); childrenMatch != nil {
			sw.Children = splitSlurmList(childrenMatch[1])
		}
		switches = append(switches, sw)
	}
	if len(switches) == 0 {
		return nil, fmt.Errorf("no switch parsed from:\n%s", strings.TrimSpace(out))
	}
	return switches, nil
}

// slurmBlock is one line of `scontrol show topology <block>`.
type slurmBlock struct {
	Name  string
	Nodes string
	Size  int
}

func parseSlurmBlocks(out string) ([]slurmBlock, error) {
	var blocks []slurmBlock
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		match := blockNamePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		block := slurmBlock{Name: match[1]}
		if nodesMatch := nodesFieldPattern.FindStringSubmatch(line); nodesMatch != nil {
			block.Nodes = nodesMatch[1]
		}
		if sizeMatch := blockSizePattern.FindStringSubmatch(line); sizeMatch != nil {
			size, err := strconv.Atoi(sizeMatch[1])
			if err != nil {
				return nil, fmt.Errorf("parse BlockSize of block %q: %w", block.Name, err)
			}
			block.Size = size
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no block parsed from:\n%s", strings.TrimSpace(out))
	}
	return blocks, nil
}

// parseScontrolBlocks splits `scontrol show <kind>` output into one chunk per object, keyed by the
// value of the field that opens each chunk (NodeName, PartitionName).
func parseScontrolBlocks(out, leadField string) map[string]string {
	blocks := make(map[string]string)
	prefix := leadField + "="

	var name string
	var current []string
	flush := func() {
		if name != "" {
			blocks[name] = strings.Join(current, "\n")
		}
		name, current = "", nil
	}

	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			flush()
			name = strings.Fields(strings.TrimPrefix(trimmed, prefix))[0]
		}
		if name != "" {
			current = append(current, trimmed)
		}
	}
	flush()

	return blocks
}

// parseNodeTopologies maps the topologies a node registered into to the unit path it joined, read
// from the Topology=<name>:<unit>[,<name>:<unit>] field of `scontrol show node`. A node that
// registered into none - a CPU-only worker, or one that is powered down - has no field at all.
func parseNodeTopologies(block string) map[string]string {
	match := topologyFieldPattern.FindStringSubmatch(block)
	if match == nil {
		return nil
	}

	units := make(map[string]string)
	for _, spec := range strings.Split(match[1], ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		name, unit, found := strings.Cut(spec, ":")
		if !found {
			continue
		}
		units[name] = unit
	}
	if len(units) == 0 {
		return nil
	}
	return units
}

// registeredLeaf returns the unit a node joined at the end of its registration path. For a tree the
// path is "<root>:...:<leaf>", for a block it is the block name.
func registeredLeaf(unit string) string {
	segments := strings.Split(unit, ":")
	return segments[len(segments)-1]
}

// partitionInfo is what `scontrol show partition` says about one partition. Slurm prints Topology=
// on every partition, including the ones that fall back to the cluster default.
type partitionInfo struct {
	Name     string
	Topology string
	Nodes    string
}

func parsePartitions(out string) []partitionInfo {
	blocks := parseScontrolBlocks(out, "PartitionName")

	partitions := make([]partitionInfo, 0, len(blocks))
	for name, block := range blocks {
		partition := partitionInfo{Name: name}
		if match := topologyFieldPattern.FindStringSubmatch(block); match != nil {
			partition.Topology = match[1]
		}
		if match := nodesFieldPattern.FindStringSubmatch(block); match != nil {
			partition.Nodes = match[1]
		}
		partitions = append(partitions, partition)
	}
	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i].Name < partitions[j].Name
	})
	return partitions
}

// srunRefused reports whether srun gave up because the request could not be placed, as opposed to
// failing for an unrelated reason. Only the placement failure proves a topology constraint.
func srunRefused(out string, err error) bool {
	if err == nil {
		return false
	}
	text := out + " " + err.Error()
	for _, marker := range []string{
		"Unable to allocate resources",
		"Requested node configuration is not available",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// halveBlockSizes returns the block sizes to test a reconfigure with. Only the base is halved,
// which keeps it a power of two and never asks for blocks larger than the ones the cluster already
// has, which slurmctld would reject. The aggregation levels above the base are carried over
// untouched, so the change under test is the base size and nothing else.
func halveBlockSizes(sizes []int) ([]int, bool) {
	if len(sizes) == 0 || sizes[0] < 2 {
		return nil, false
	}
	halved := slices.Clone(sizes)
	halved[0] = sizes[0] / 2
	return halved, true
}

// splitSlurmList splits the comma-separated lists Slurm uses for switch children. Node lists are
// left alone: they are hostlists and need scontrol to expand.
func splitSlurmList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(null)" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// plainHostlist splits a hostlist that needs no expansion, reporting false when it carries a "["
// range that only scontrol can resolve.
func plainHostlist(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "(null)" {
		return nil, true
	}
	if strings.Contains(value, "[") {
		return nil, false
	}
	return splitSlurmList(value), true
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
