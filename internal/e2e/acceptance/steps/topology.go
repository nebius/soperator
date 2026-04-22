package steps

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const topologyJobTimeout = 10 * time.Minute

var (
	topologySwitchNamePattern = regexp.MustCompile(`SwitchName=(\S+)`)
	topologyLevelPattern      = regexp.MustCompile(`Level=(\d+)`)
	topologyNodesPattern      = regexp.MustCompile(`Nodes=(\S+)`)
	topologySwitchesPattern   = regexp.MustCompile(`Switches=(\S+)`)
)

type topologyNode struct {
	level    int
	isRoot   bool
	isWorker bool
	targets  map[string]bool
}

// topologyGraph is a directed edge-set view of `scontrol show topology`.
type topologyGraph struct {
	nodes map[string]*topologyNode
}

type Topology struct {
	exec  framework.Exec
	state *framework.ClusterState

	graph         *topologyGraph
	reportedAddrs map[string]string
}

func NewTopology(state *framework.ClusterState, exec framework.Exec) *Topology {
	return &Topology{exec: exec, state: state}
}

func (s *Topology) Register(sc *godog.ScenarioContext) {
	sc.Step(`^the Slurm topology plugin is topology/tree$`, s.theSlurmTopologyPluginIsTree)
	sc.Step(`^scontrol show topology is parsed into a switch tree$`, s.scontrolTopologyIsParsedIntoASwitchTree)
	sc.Step(`^every worker in the main partition is present in the topology$`, s.everyWorkerIsPresentInTheTopology)
	sc.Step(`^a job runs on all available workers and reports SLURM_TOPOLOGY_ADDR$`, s.aJobRunsOnAllAvailableWorkers)
	sc.Step(`^each task's SLURM_TOPOLOGY_ADDR matches its position in the topology$`, s.eachTaskAddrMatchesTheTree)
}

func (s *Topology) theSlurmTopologyPluginIsTree(ctx context.Context) error {
	out, err := framework.ExecControllerWithDefaultRetry(ctx, s.exec,
		`scontrol show config | awk -F'= *' '/^TopologyPlugin /{print $2; exit}'`)
	if err != nil {
		return fmt.Errorf("read TopologyPlugin from scontrol: %w", err)
	}
	plugin := strings.TrimSpace(out)
	if plugin != "topology/tree" {
		s.exec.Logf("topology plugin is %q, skipping scenario", plugin)
		return godog.ErrSkip
	}
	return nil
}

func (s *Topology) scontrolTopologyIsParsedIntoASwitchTree(ctx context.Context) error {
	raw, err := framework.ExecControllerWithDefaultRetry(ctx, s.exec, "scontrol show topology")
	if err != nil {
		return fmt.Errorf("scontrol show topology: %w", err)
	}
	s.exec.Logf("scontrol show topology:\n%s", strings.TrimSpace(raw))

	workerNames := make([]string, 0, len(s.state.Workers))
	for _, w := range s.state.Workers {
		workerNames = append(workerNames, w.Name)
	}

	expand := func(hostlist string) ([]string, error) {
		cmd := fmt.Sprintf("scontrol show hostnames %s", framework.ShellQuote(hostlist))
		out, err := framework.ExecControllerWithDefaultRetry(ctx, s.exec, cmd)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				names = append(names, line)
			}
		}
		return names, nil
	}

	graph, err := parseTopology(raw, workerNames, expand)
	if err != nil {
		return fmt.Errorf("parse topology: %w", err)
	}
	s.graph = graph
	return nil
}

func (s *Topology) everyWorkerIsPresentInTheTopology() error {
	if s.graph == nil {
		return fmt.Errorf("topology graph not parsed yet")
	}
	var missing []string
	for _, worker := range s.state.Workers {
		n := s.graph.nodes[worker.Name]
		if n == nil || !n.isWorker {
			missing = append(missing, worker.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("workers missing from topology: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (s *Topology) aJobRunsOnAllAvailableWorkers(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, topologyJobTimeout)
	defer cancel()

	if len(s.state.Workers) == 0 {
		return fmt.Errorf("no workers discovered")
	}
	// We target every discovered worker without re-filtering by Slurm state.
	// The acceptance suite expects the cluster it owns to have all nodes
	// IDLE at this point; a drained/down/suspended node would be a symptom
	// of a prior failure worth surfacing, not something to silently skip.
	// If the cluster is genuinely in a bad state, the srun call fails within
	// the 10-minute context timeout rather than hanging.
	names := make([]string, 0, len(s.state.Workers))
	for _, w := range s.state.Workers {
		names = append(names, w.Name)
	}

	inner := `printf "%s %s\n" "$SLURMD_NODENAME" "$SLURM_TOPOLOGY_ADDR"`
	cmd := fmt.Sprintf("srun -N %d -w %s --time=1:00 --cpu-bind=none bash -c %s",
		len(names),
		framework.ShellQuote(strings.Join(names, ",")),
		framework.ShellQuote(inner))
	out, err := s.exec.ExecJail(ctx, cmd)
	s.exec.Logf("srun topology addrs output:\n%s", strings.TrimSpace(out))
	if err != nil {
		return fmt.Errorf("srun for topology addrs: %w", err)
	}

	s.reportedAddrs = make(map[string]string, len(names))
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		s.reportedAddrs[parts[0]] = parts[1]
	}
	if len(s.reportedAddrs) != len(names) {
		return fmt.Errorf("collected %d topology addrs from %d workers\noutput:\n%s",
			len(s.reportedAddrs), len(names), strings.TrimSpace(out))
	}
	return nil
}

func (s *Topology) eachTaskAddrMatchesTheTree() error {
	if s.graph == nil {
		return fmt.Errorf("topology graph not parsed yet")
	}
	if len(s.reportedAddrs) == 0 {
		return fmt.Errorf("no SLURM_TOPOLOGY_ADDR values collected")
	}
	var mismatches []string
	for worker, reported := range s.reportedAddrs {
		if err := validateReportedPath(s.graph, reported, worker); err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s: %v", worker, err))
		}
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("topology address mismatches:\n  %s", strings.Join(mismatches, "\n  "))
	}
	return nil
}

// hostlistExpander turns a Slurm hostlist value that contains `[` into the
// concrete list of hostnames. Production wires it to `scontrol show hostnames`;
// tests with plain comma-separated values can pass nil.
type hostlistExpander func(value string) ([]string, error)

// parseTopology turns `scontrol show topology` output into a topologyGraph.
// `allWorkers` is consulted only when a switch declares `Nodes=ALL`. `expand`
// is only called for values containing `[`; plain comma-separated lists are
// split in-process.
func parseTopology(raw string, allWorkers []string, expand hostlistExpander) (*topologyGraph, error) {
	graph := &topologyGraph{nodes: make(map[string]*topologyNode)}

	switchNames := make(map[string]bool)
	childSwitches := make(map[string]bool)
	var parseErrs []error

	ensure := func(name string) *topologyNode {
		n := graph.nodes[name]
		if n == nil {
			n = &topologyNode{}
			graph.nodes[name] = n
		}
		return n
	}
	addEdge := func(source, target string) {
		src := ensure(source)
		if src.targets == nil {
			src.targets = make(map[string]bool)
		}
		src.targets[target] = true
	}
	maybeExpand := func(value string) ([]string, error) {
		if strings.Contains(value, "[") {
			if expand == nil {
				return nil, fmt.Errorf("hostlist expansion required for %q but no expander configured", value)
			}
			return expand(value)
		}
		var out []string
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out, nil
	}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := topologySwitchNamePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switchName := m[1]
		if strings.Contains(switchName, ".") {
			// $SLURM_TOPOLOGY_ADDR is dot-separated, so a dotted switch
			// name is ambiguous with level boundaries. Reject explicitly
			// rather than silently mis-splitting during validation.
			parseErrs = append(parseErrs, fmt.Errorf("switch name contains '.' which is not supported: %q", switchName))
			continue
		}
		switchNames[switchName] = true
		ensure(switchName)

		if lm := topologyLevelPattern.FindStringSubmatch(line); lm != nil {
			lvl, err := strconv.Atoi(lm[1])
			if err != nil {
				parseErrs = append(parseErrs, fmt.Errorf("parse Level in %q: %w", line, err))
				continue
			}
			graph.nodes[switchName].level = lvl
		}

		if sm := topologySwitchesPattern.FindStringSubmatch(line); sm != nil && sm[1] != "(null)" {
			children, err := maybeExpand(sm[1])
			if err != nil {
				parseErrs = append(parseErrs, fmt.Errorf("expand Switches %q: %w", sm[1], err))
				continue
			}
			for _, child := range children {
				if strings.Contains(child, ".") {
					parseErrs = append(parseErrs, fmt.Errorf("child switch name contains '.' which is not supported: %q", child))
					continue
				}
				addEdge(switchName, child)
				childSwitches[child] = true
			}
		}

		if nm := topologyNodesPattern.FindStringSubmatch(line); nm != nil {
			value := nm[1]
			var nodes []string
			switch value {
			case "", "(null)":
				// no attached nodes
			case "ALL":
				nodes = allWorkers
			default:
				expanded, err := maybeExpand(value)
				if err != nil {
					parseErrs = append(parseErrs, fmt.Errorf("expand Nodes %q: %w", value, err))
					continue
				}
				nodes = expanded
			}
			for _, n := range nodes {
				addEdge(switchName, n)
			}
		}
	}

	if len(parseErrs) > 0 {
		return nil, errors.Join(parseErrs...)
	}
	if len(switchNames) == 0 {
		return nil, fmt.Errorf("no switches parsed from topology output")
	}

	// Switches that never appear as a child become roots; edge targets that
	// aren't themselves switches become workers.
	for name := range switchNames {
		if !childSwitches[name] {
			graph.nodes[name].isRoot = true
		}
	}
	rootCount := 0
	for _, n := range graph.nodes {
		if n.isRoot {
			rootCount++
		}
	}
	if rootCount == 0 {
		return nil, fmt.Errorf("no root switch found in topology (all switches appear as children)")
	}

	for _, n := range graph.nodes {
		for target := range n.targets {
			if !switchNames[target] {
				tn := ensure(target)
				tn.isWorker = true
			}
		}
	}

	return graph, nil
}

// validateReportedPath accepts a $SLURM_TOPOLOGY_ADDR reported by srun and
// verifies it is a valid walk through the parsed graph. Slurm emits one
// segment per level (with empty segments for skipped levels), so the path
// has exactly rootLevel+2 segments: the root, one slot per intermediate
// level, and the worker at the tail. Anchoring each non-empty switch to
// its declared Level catches truncated paths like "root.worker-0" where
// the worker slips into a switch slot.
func validateReportedPath(g *topologyGraph, reported, worker string) error {
	segments := strings.Split(reported, ".")
	if len(segments) < 2 {
		return fmt.Errorf("malformed path %q", reported)
	}
	if segments[len(segments)-1] != worker {
		return fmt.Errorf("path %q does not end with worker %q", reported, worker)
	}

	rootName := segments[0]
	if rootName == "" {
		return fmt.Errorf("path %q starts with empty root segment", reported)
	}
	root := g.nodes[rootName]
	if root == nil || !root.isRoot {
		return fmt.Errorf("path %q does not start at a root switch (got %q)", reported, rootName)
	}
	wantSegments := root.level + 2
	if len(segments) != wantSegments {
		return fmt.Errorf("path %q has %d segments, expected %d (root %q is at level %d)",
			reported, len(segments), wantSegments, rootName, root.level)
	}
	// The leaf slot (just before the worker) must be filled. Slurm always
	// emits the worker's direct leaf switch there; an empty slot would mean
	// the path skipped the leaf level entirely, e.g. "root...worker-0",
	// which would otherwise slip through because ancestor switches list
	// descendant workers in their Nodes= field.
	if segments[len(segments)-2] == "" {
		return fmt.Errorf("path %q: leaf slot at position %d is empty", reported, len(segments)-2)
	}

	var prev *topologyNode
	var prevName string
	for i := 0; i < len(segments)-1; i++ {
		name := segments[i]
		if name == "" {
			continue
		}
		n := g.nodes[name]
		if n == nil || n.isWorker {
			return fmt.Errorf("path %q: unknown switch %q at position %d", reported, name, i)
		}
		if n.level != root.level-i {
			return fmt.Errorf("path %q: switch %q at position %d has level %d, expected %d",
				reported, name, i, n.level, root.level-i)
		}
		if prev != nil && !prev.targets[name] {
			return fmt.Errorf("path %q: no edge %s -> %s in topology", reported, prevName, name)
		}
		prev, prevName = n, name
	}

	if prev == nil || !prev.targets[worker] {
		return fmt.Errorf("path %q: worker %s is not attached to switch %s", reported, worker, prevName)
	}
	return nil
}
