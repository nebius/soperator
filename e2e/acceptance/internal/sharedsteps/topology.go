package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	topologyConfigBaseName = "topology-config"
	topologyConfigMapKey   = "topology.yaml"
	topologyJailPath       = "/etc/slurm/topology.yaml"
	topologyStructureKey   = "topology.slurm.nebius.ai/structure"

	// topologyJobImmediate bounds how long srun waits for a placement that is expected to happen,
	// topologyDenyImmediate how long it waits for one that must not. The deny case is the shorter
	// of the two: it is the scenario's failure mode to wait out a queue that will never be served.
	topologyJobImmediate  = 90 * time.Second
	topologyDenyImmediate = 30 * time.Second

	topologyConvergeTimeout = 10 * time.Minute
	topologyCleanupTimeout  = 10 * time.Minute
)

// powerStateFlags are the node state flags of a worker that has no slurmd running, so it carries no
// topology registration and cannot take a job without being powered up first.
var powerStateFlags = []string{"POWERED_DOWN", "POWERING_UP", "POWERING_DOWN", "POWER_DOWN"}

// slurmNodeView is what `scontrol show node` says about one worker.
type slurmNodeView struct {
	Info framework.SlurmNodeInfo
	// Units maps every topology the node registered into to the unit path it joined.
	Units map[string]string
}

// Ready reports whether the worker is healthy and running slurmd, so it carries a topology
// registration. Powered-down workers are excluded rather than woken up: a scenario that powers a
// node on measures the power-up path, not topology.
func (n slurmNodeView) Ready() bool {
	if !n.Info.IsUsable() {
		return false
	}
	for _, flag := range powerStateFlags {
		if n.Info.HasStateFlag(flag) {
			return false
		}
	}
	return true
}

// Idle reports whether the worker can take a job right now. Ready is not enough for that: a worker
// running someone else's job is ALLOCATED or MIXED, which IsUsable accepts, and asking for it would
// make a scenario wait out that job.
func (n slurmNodeView) Idle() bool {
	return n.Ready() && n.Info.HasStateFlag("IDLE")
}

// leafPair is two workers of one tree topology, together with the leaf switches Slurm says they
// hang off. The switches are read back from the running cluster rather than assumed, so the
// scenario only claims to test a cross-switch placement when it really has one.
type leafPair struct {
	Partition string
	Topology  string
	Nodes     [2]string
	Leaves    [2]string
}

// allocationResult is what one srun of the switch-limit check returned, kept between the step that
// requests the allocation and the step that judges it.
type allocationResult struct {
	Output    string
	Err       error
	Requested bool
}

type Topology struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	configName string
	rendered   []topologyEntry
	loaded     []topologyEntry
	snapshot   framework.WorkerSnapshot
	nodes      map[string]slurmNodeView
	partitions []partitionInfo
	hostlists  map[string][]string

	treeTopology string
	crossLeaf    *leafPair
	sameLeaf     *leafPair
	crossLeafRun allocationResult

	blockTopology     string
	blockSizesRestore []int
}

func NewTopology(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *Topology {
	return &Topology{
		info:     info,
		runtime:  runtime,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *Topology) RegisterSteps(sc *godog.ScenarioContext) {
	// The configuration steps come first in every scenario and are the only ones that skip: a
	// cluster that does not ask for the capability under test has nothing to prove. Past them, a
	// missing or unusable resource is a failure.
	sc.Step(`^the cluster is configured with several named topologies$`, s.clusterIsConfiguredWithNamedTopologies)
	sc.Step(`^the cluster is configured with a tree topology spanning multiple leaf switches$`, s.clusterIsConfiguredWithTreeTopology)
	sc.Step(`^the cluster is configured with a block topology whose base size can be changed$`, s.clusterIsConfiguredWithResizableBlockTopology)

	sc.Step(`^the operator published the topology config$`, s.publishedTopologyConfig)
	sc.Step(`^the config declares several named topologies$`, s.configDeclaresSeveralTopologies)
	sc.Step(`^exactly one of them is the cluster default$`, s.exactlyOneClusterDefault)
	sc.Step(`^one of them is a flat topology$`, s.oneOfThemIsFlat)
	sc.Step(`^the topology config is delivered to the jail$`, s.configIsDeliveredToTheJail)
	sc.Step(`^every GPU worker is listed by a fabric topology$`, s.gpuWorkersAreListed)
	sc.Step(`^no CPU-only worker is listed by a fabric topology$`, s.cpuWorkersAreNotListed)
	sc.Step(`^Slurm is asked which topologies it loaded$`, s.readLoadedTopologies)
	sc.Step(`^Slurm loaded exactly the topologies the operator rendered$`, s.loadedMatchesRendered)
	sc.Step(`^every partition is bound to a topology Slurm loaded$`, s.partitionsAreBound)
	sc.Step(`^every running worker is registered into the topologies that list it$`, s.registrationsMatchTheConfig)
	sc.Step(`^a job runs in a partition of every topology$`, s.jobRunsInEveryTopology)

	sc.Step(`^Slurm loaded the tree topology$`, s.slurmLoadedTheTreeTopology)
	sc.Step(`^two configured workers on different leaf switches are requested without a switch limit$`, s.requestCrossLeafWorkers)
	sc.Step(`^the cross-leaf allocation succeeds$`, s.crossLeafAllocationSucceeds)
	sc.Step(`^the same workers are requested with at most one switch$`, s.requestCrossLeafWorkersWithinOneSwitch)
	sc.Step(`^the cross-leaf allocation is rejected$`, s.crossLeafAllocationIsRejected)
	sc.Step(`^two workers on the same leaf switch are still allocated within one switch$`, s.sameLeafAllocationSucceeds)

	sc.Step(`^Slurm reports the blocks the operator rendered$`, s.slurmReportsRenderedBlocks)
	sc.Step(`^a job runs in a partition of the block topology$`, s.jobRunsInBlockPartition)
	sc.Step(`^the block topology base size is halved$`, s.halveBlockTopologySize)
	sc.Step(`^the rendered config carries the new block sizes$`, s.renderedConfigCarriesBlockSizes)
	sc.Step(`^Slurm reports the new base block size$`, s.slurmReportsBaseBlockSize)
	sc.Step(`^the topology JailedConfig has no pending reconfigure request$`, s.noPendingReconfigureRequest)
}

func (s *Topology) CleanupAndReset(ctx context.Context) {
	if s.blockSizesRestore != nil {
		cleanupCtx, cancel := context.WithTimeout(ctx, topologyCleanupTimeout)
		defer cancel()

		sizes := s.blockSizesRestore
		s.blockSizesRestore = nil
		s.runtime.Logf("cleanup: restoring blockSizes %v of topology %s", sizes, s.blockTopology)
		// The wait covers slurmctld too, not just the published file: leaving the cluster running
		// the test sizes would fail the next run before it changed anything.
		if err := s.patchBlockSizes(cleanupCtx, s.blockTopology, sizes); err != nil {
			s.runtime.Logf("cleanup: restore blockSizes of topology %s: %v", s.blockTopology, err)
		} else if err := s.waitForRenderedBlockSizes(cleanupCtx, s.blockTopology, sizes); err != nil {
			s.runtime.Logf("cleanup: wait for restored blockSizes of topology %s: %v", s.blockTopology, err)
		} else if err := s.waitForSlurmBaseBlockSize(cleanupCtx, s.blockTopology, sizes[0]); err != nil {
			s.runtime.Logf("cleanup: wait for slurmctld to pick the restored blockSizes of topology %s up: %v",
				s.blockTopology, err)
		}
	}

	s.configName = ""
	s.rendered = nil
	s.loaded = nil
	s.snapshot = framework.WorkerSnapshot{}
	s.nodes = nil
	s.partitions = nil
	s.hostlists = nil
	s.treeTopology = ""
	s.crossLeaf = nil
	s.sameLeaf = nil
	s.crossLeafRun = allocationResult{}
	s.blockTopology = ""
}

// region configuration

// configuredTopologies returns the named topologies the SlurmCluster asks for. It reads the spec,
// not what the operator produced: a cluster that configures nothing is a cluster the scenario has
// no business running on, and that has to be decided before anything is published.
func (s *Topology) configuredTopologies(ctx context.Context) ([]kubeobjects.NamedTopology, error) {
	cluster, err := s.readSlurmCluster(ctx)
	if err != nil {
		return nil, err
	}
	if cluster.Spec.Topology == nil {
		return nil, nil
	}
	return cluster.Spec.Topology.Topologies, nil
}

func (s *Topology) clusterIsConfiguredWithNamedTopologies(ctx context.Context) error {
	topologies, err := s.configuredTopologies(ctx)
	if err != nil {
		return err
	}
	if len(topologies) < 2 {
		s.runtime.Logf("acceptance: SlurmCluster %s configures %d named topologies, skipping scenario",
			s.info.SlurmClusterName, len(topologies))
		return godog.ErrSkip
	}
	return nil
}

// leafSwitches counts the switches of a tree that carry workers. The root and the intermediate
// switches are not what the switch limit needs: a tree of "root + one leaf" is a valid
// configuration, it just holds every worker within one switch and can demonstrate no constraint.
// A rendered switch carries either children or nodes, never both, so a node list marks a leaf.
func leafSwitches(entry topologyEntry) int {
	var leaves int
	for _, sw := range entry.Switches {
		if strings.TrimSpace(sw.Nodes) != "" {
			leaves++
		}
	}
	return leaves
}

// clusterIsConfiguredWithTreeTopology skips unless the cluster asks for a tree that really branches.
// The number of leaf switches is not in the spec - the operator derives it from the fabric the
// workers sit on - so it is read back from the published config, which is still configuration
// rather than running state.
func (s *Topology) clusterIsConfiguredWithTreeTopology(ctx context.Context) error {
	topologies, err := s.configuredTopologies(ctx)
	if err != nil {
		return err
	}
	var configured []string
	for _, topology := range topologies {
		if topology.Topo.Type == topologyKindTree {
			configured = append(configured, topology.Name)
		}
	}
	if len(configured) == 0 {
		s.runtime.Logf("acceptance: SlurmCluster %s configures no tree topology, skipping scenario",
			s.info.SlurmClusterName)
		return godog.ErrSkip
	}

	if err := s.loadRenderedConfig(ctx); err != nil {
		return err
	}
	for _, name := range configured {
		entry, ok := topologyByName(s.rendered, name)
		if !ok {
			return fmt.Errorf("tree topology %s is configured but missing from the published config", name)
		}
		if leafSwitches(entry) > 1 {
			s.treeTopology = name
			return nil
		}
	}

	s.runtime.Logf("acceptance: no configured tree topology (%s) spans more than one leaf switch, skipping scenario",
		strings.Join(configured, ", "))
	return godog.ErrSkip
}

// clusterIsConfiguredWithResizableBlockTopology skips unless the cluster asks for a block topology
// whose base size can be halved. Both are configuration: a base of one leaves no smaller valid
// configuration to reconfigure into.
func (s *Topology) clusterIsConfiguredWithResizableBlockTopology(ctx context.Context) error {
	topologies, err := s.configuredTopologies(ctx)
	if err != nil {
		return err
	}
	var declared []string
	for _, topology := range topologies {
		if topology.Topo.Type != topologyKindBlock {
			continue
		}
		declared = append(declared, fmt.Sprintf("%s%v", topology.Name, topology.Topo.BlockSizes))
		if _, ok := halveBlockSizes(topology.Topo.BlockSizes); ok {
			s.blockTopology = topology.Name
			return nil
		}
	}

	if len(declared) == 0 {
		s.runtime.Logf("acceptance: SlurmCluster %s configures no block topology, skipping scenario",
			s.info.SlurmClusterName)
	} else {
		s.runtime.Logf("acceptance: no configured block topology has a base size that can be halved (%s), skipping scenario",
			strings.Join(declared, ", "))
	}
	return godog.ErrSkip
}

// region config

// loadRenderedConfig reads the published topology config once per scenario.
func (s *Topology) loadRenderedConfig(ctx context.Context) error {
	if s.rendered != nil {
		return nil
	}
	name, rendered, err := s.readTopologyConfigMap(ctx)
	if err != nil {
		return err
	}
	s.configName = name
	s.runtime.Logf("rendered %s of ConfigMap %s:\n%s", topologyConfigMapKey, name, strings.TrimSpace(rendered))

	entries, err := parseTopologyEntries(rendered)
	if err != nil {
		return fmt.Errorf("parse rendered topology config: %w", err)
	}
	s.rendered = entries
	return nil
}

func (s *Topology) publishedTopologyConfig(ctx context.Context) error {
	return s.loadRenderedConfig(ctx)
}

func (s *Topology) configIsDeliveredToTheJail(ctx context.Context) error {
	delivered, err := s.runtime.Jail().RunWithDefaultRetry(ctx,
		fmt.Sprintf("cat %s", framework.ShellQuote(topologyJailPath)))
	if err != nil {
		return fmt.Errorf("read the topology config delivered to the jail: %w", err)
	}
	deliveredEntries, err := parseTopologyEntries(delivered)
	if err != nil {
		return fmt.Errorf("parse the topology config delivered to the jail: %w", err)
	}
	if got, want := topologyStructure(deliveredEntries), topologyStructure(s.rendered); got != want {
		return fmt.Errorf("the topology config in the jail is %q, the operator published %q", got, want)
	}
	return nil
}

// readTopologyConfigMap returns the name of the topology ConfigMap and its rendered body. Clusters
// created before prefixed naming keep the ConfigMap under its bare name, so both are tried.
func (s *Topology) readTopologyConfigMap(ctx context.Context) (string, string, error) {
	candidates := []string{
		framework.ClusterPrefixedName(s.info.SlurmClusterName, topologyConfigBaseName),
		topologyConfigBaseName,
	}

	var failures []error
	for _, name := range candidates {
		out, err := s.runtime.Kubectl().Run(ctx,
			"get", "configmap", name,
			"-n", framework.SoperatorNamespace,
			"-o", `jsonpath={.data.topology\.yaml}`,
		)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if strings.TrimSpace(out) == "" {
			failures = append(failures, fmt.Errorf("ConfigMap %s has no %s", name, topologyConfigMapKey))
			continue
		}
		return name, out, nil
	}
	return "", "", fmt.Errorf("read the topology config of cluster %s; a cluster that declares no topology publishes none: %w",
		s.info.SlurmClusterName, errors.Join(failures...))
}

func (s *Topology) configDeclaresSeveralTopologies() error {
	if len(s.rendered) < 2 {
		return fmt.Errorf("the config declares %d topologies (%s), want at least two",
			len(s.rendered), strings.Join(topologyNames(s.rendered), ", "))
	}
	return nil
}

func (s *Topology) exactlyOneClusterDefault() error {
	var defaults []string
	for _, entry := range s.rendered {
		if entry.ClusterDefault {
			defaults = append(defaults, entry.Name)
		}
	}
	if len(defaults) != 1 {
		return fmt.Errorf("%d topologies are the cluster default (%s), want exactly one",
			len(defaults), strings.Join(defaults, ", "))
	}
	return nil
}

func (s *Topology) oneOfThemIsFlat() error {
	for _, entry := range s.rendered {
		if entry.Kind == topologyKindFlat {
			return nil
		}
	}
	return fmt.Errorf("no flat topology among %s; workers with no fabric have nothing to fall back to",
		strings.Join(topologyNames(s.rendered), ", "))
}

// region workers

func (s *Topology) gpuWorkersAreListed(ctx context.Context) error {
	if err := s.loadWorkers(ctx); err != nil {
		return err
	}
	if len(s.snapshot.GPUWorkers) == 0 {
		s.runtime.Logf("acceptance: the cluster has no GPU worker, nothing for a fabric topology to list")
		return nil
	}

	listed, err := s.fabricNodes(ctx)
	if err != nil {
		return err
	}

	var missing []string
	for _, worker := range s.snapshot.GPUWorkers {
		if _, ok := listed[worker.Name]; !ok {
			missing = append(missing, worker.Name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("GPU workers are listed by no tree or block topology: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (s *Topology) cpuWorkersAreNotListed(ctx context.Context) error {
	if err := s.loadWorkers(ctx); err != nil {
		return err
	}

	listed, err := s.fabricNodes(ctx)
	if err != nil {
		return err
	}

	var unexpected []string
	for _, worker := range s.snapshot.CPUWorkers {
		if topology, ok := listed[worker.Name]; ok {
			unexpected = append(unexpected, fmt.Sprintf("%s in %s", worker.Name, topology))
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		// Placing a CPU-only worker under a switch tells the scheduler it is one hop from the GPU
		// nodes, and a block topology refuses to allocate a partition that mixes the two.
		return fmt.Errorf("CPU-only workers sit on a fabric topology: %s", strings.Join(unexpected, ", "))
	}
	return nil
}

// fabricNodes maps every node the tree and block topologies list to the first topology listing it.
func (s *Topology) fabricNodes(ctx context.Context) (map[string]string, error) {
	listed := make(map[string]string)
	for _, entry := range s.rendered {
		nodes, err := s.topologyNodes(ctx, entry)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if _, ok := listed[node]; !ok {
				listed[node] = entry.Name
			}
		}
	}
	return listed, nil
}

// region loaded state

func (s *Topology) readLoadedTopologies(ctx context.Context) error {
	// topoconf prints every topology slurmctld parsed, in the format of topology.yaml itself.
	// "scontrol show topology" without a name prints only the cluster default, so it cannot answer
	// this question.
	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show topoconf")
	if err != nil {
		return fmt.Errorf("scontrol show topoconf: %w", err)
	}
	s.runtime.Logf("scontrol show topoconf:\n%s", strings.TrimSpace(out))

	entries, err := parseTopologyEntries(out)
	if err != nil {
		return fmt.Errorf("parse scontrol show topoconf: %w", err)
	}
	s.loaded = entries
	return nil
}

func (s *Topology) loadedMatchesRendered() error {
	if got, want := topologyStructure(s.loaded), topologyStructure(s.rendered); got != want {
		return fmt.Errorf("slurmctld loaded %q, the operator rendered %q", got, want)
	}
	return nil
}

func (s *Topology) partitionsAreBound(ctx context.Context) error {
	if err := s.loadPartitions(ctx); err != nil {
		return err
	}

	var problems []string
	for _, partition := range s.partitions {
		if partition.Topology == "" {
			problems = append(problems, fmt.Sprintf("%s is bound to no topology", partition.Name))
			continue
		}
		if _, ok := topologyByName(s.loaded, partition.Topology); !ok {
			problems = append(problems, fmt.Sprintf("%s is bound to unknown topology %s", partition.Name, partition.Topology))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("partition bindings do not resolve: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *Topology) registrationsMatchTheConfig(ctx context.Context) error {
	if err := s.loadWorkers(ctx); err != nil {
		return err
	}
	if err := s.loadNodes(ctx); err != nil {
		return err
	}

	units, err := s.topologyUnitsByNode(ctx)
	if err != nil {
		return err
	}

	var problems []string
	for _, worker := range s.snapshot.Workers {
		node, ok := s.nodes[worker.Name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s is missing from scontrol show node", worker.Name))
			continue
		}
		if !node.Ready() {
			// A worker registers into its topologies when its pod starts, so one that is powered
			// down or draining legitimately carries no registration at all.
			s.runtime.Logf("acceptance: worker %s is not running (state=%s), skipping its registration",
				worker.Name, node.Info.State)
			continue
		}

		want := make(map[string]struct{})
		for topology, unit := range units[worker.Name] {
			want[topology] = struct{}{}
			if registered, ok := node.Units[topology]; ok && registeredLeaf(registered) != unit {
				problems = append(problems, fmt.Sprintf("%s joined %s at %q, the config places it at %q",
					worker.Name, topology, registeredLeaf(registered), unit))
			}
		}

		got := make(map[string]struct{}, len(node.Units))
		for topology := range node.Units {
			got[topology] = struct{}{}
		}
		if !sameStringSet(got, want) {
			problems = append(problems, fmt.Sprintf("%s is registered into [%s], the config lists it in [%s]",
				worker.Name,
				strings.Join(sortedKeys(got), " "),
				strings.Join(sortedKeys(want), " ")))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("worker registrations disagree with the rendered config:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// topologyUnitsByNode maps every node the rendered config places to the unit it is placed in, per
// topology: the leaf switch for a tree, the block for a block topology.
func (s *Topology) topologyUnitsByNode(ctx context.Context) (map[string]map[string]string, error) {
	units := make(map[string]map[string]string)
	record := func(node, topology, unit string) {
		if units[node] == nil {
			units[node] = make(map[string]string)
		}
		units[node][topology] = unit
	}

	for _, entry := range s.rendered {
		switch entry.Kind {
		case topologyKindTree:
			for _, sw := range entry.Switches {
				nodes, err := s.expandHostlist(ctx, sw.Nodes)
				if err != nil {
					return nil, err
				}
				for _, node := range nodes {
					record(node, entry.Name, sw.Name)
				}
			}
		case topologyKindBlock:
			for _, block := range entry.Blocks {
				nodes, err := s.expandHostlist(ctx, block.Nodes)
				if err != nil {
					return nil, err
				}
				for _, node := range nodes {
					record(node, entry.Name, block.Name)
				}
			}
		}
	}
	return units, nil
}

// region jobs

func (s *Topology) jobRunsInEveryTopology(ctx context.Context) error {
	if err := s.loadPartitions(ctx); err != nil {
		return err
	}
	if err := s.loadNodes(ctx); err != nil {
		return err
	}

	for _, entry := range s.rendered {
		if err := s.runJobInTopology(ctx, entry.Name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Topology) runJobInTopology(ctx context.Context, topology string) error {
	partition, node, ok, err := s.pickReadyNodeOfTopology(ctx, topology)
	if err != nil {
		return err
	}
	if !ok {
		if _, bound := s.partitionOfTopology(topology); !bound {
			return fmt.Errorf("topology %s is bound to no partition, so no job can ever run in it", topology)
		}
		return fmt.Errorf("no worker of a partition of topology %s is running, so its job cannot be placed:\n%s",
			topology, s.topologyWorkerReport(ctx, topology))
	}

	out, err := s.srun(ctx, srunRequest{
		Partition: partition,
		Nodes:     []string{node},
		Immediate: topologyJobImmediate,
	})
	if err != nil {
		return fmt.Errorf("run a job on %s in partition %s of topology %s: %w", node, partition, topology, err)
	}
	if !strings.Contains(out, node) {
		return fmt.Errorf("the job of topology %s ran somewhere other than %s:\n%s", topology, node, strings.TrimSpace(out))
	}
	return nil
}

// pickReadyNodeOfTopology picks the worker the topology's job runs on, preferring an idle one so
// the job does not queue behind whatever that worker is already running. A busy worker is still
// accepted as a fallback: the job is a short hostname, and waiting for it is better than declaring
// a topology unusable because its only worker happens to be occupied.
func (s *Topology) pickReadyNodeOfTopology(ctx context.Context, topology string) (string, string, bool, error) {
	var (
		fallbackPartition string
		fallbackNode      string
		haveFallback      bool
	)
	for _, partition := range s.partitions {
		if partition.Topology != topology {
			continue
		}
		nodes, err := s.expandHostlist(ctx, partition.Nodes)
		if err != nil {
			return "", "", false, err
		}
		for _, node := range nodes {
			view, ok := s.nodes[node]
			if !ok || !view.Ready() {
				continue
			}
			if view.Idle() {
				return partition.Name, node, true, nil
			}
			if !haveFallback {
				fallbackPartition, fallbackNode, haveFallback = partition.Name, node, true
			}
		}
	}
	return fallbackPartition, fallbackNode, haveFallback, nil
}

// topologyWorkerReport lists the workers of every partition bound to the topology with the state
// that kept them out, so the failure above says which workers were unusable and why.
func (s *Topology) topologyWorkerReport(ctx context.Context, topology string) string {
	var lines []string
	for _, partition := range s.partitions {
		if partition.Topology != topology {
			continue
		}
		nodes, err := s.expandHostlist(ctx, partition.Nodes)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  expand the nodes of partition %s: %v", partition.Name, err))
			continue
		}
		for _, node := range nodes {
			view, ok := s.nodes[node]
			if !ok {
				lines = append(lines, fmt.Sprintf("  %s is missing from scontrol show node", node))
				continue
			}
			lines = append(lines, fmt.Sprintf("  %s of partition %s state=%s", node, partition.Name, view.Info.State))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// region switch limit

// slurmLoadedTheTreeTopology confirms slurmctld parsed the tree the cluster configures. Past the
// configuration step a missing topology is a failure: the operator was asked for it.
func (s *Topology) slurmLoadedTheTreeTopology(ctx context.Context) error {
	if err := s.readLoadedTopologies(ctx); err != nil {
		return err
	}
	if _, ok := topologyByName(s.loaded, s.treeTopology); !ok {
		return fmt.Errorf("slurmctld loaded %s, the configured tree topology %s is not among them",
			strings.Join(topologyNames(s.loaded), ", "), s.treeTopology)
	}
	return nil
}

// pickWorkersOnDifferentLeaves finds the pair the switch limit is measured on, and is where the
// scenario decides whether it can run at all.
//
// The two outcomes are kept apart on purpose. The cluster configures a tree that branches, so a
// running cluster that separates none of its workers is a defect - they are missing, unusable or
// registered somewhere else - and fails here. A cluster whose workers are merely busy proves
// nothing about the topology either way, so it skips, and it skips before any job is submitted
// rather than after one times out.
func (s *Topology) pickWorkersOnDifferentLeaves(ctx context.Context) error {
	if err := s.loadPartitions(ctx); err != nil {
		return err
	}
	if err := s.loadNodes(ctx); err != nil {
		return err
	}

	partition, ok := s.partitionOfTopology(s.treeTopology)
	if !ok {
		return fmt.Errorf("tree topology %s is bound to no partition, no job can exercise it", s.treeTopology)
	}
	leaves, nodesByLeaf, err := s.collectLeafMembership(ctx, s.treeTopology, partition)
	if err != nil {
		return err
	}

	if running, _ := pickLeafPairs(leaves, nodesByLeaf, s.treeTopology, partition, func(string) bool { return true }); running == nil {
		return fmt.Errorf("tree topology %s spans several leaf switches, but no two of its running workers sit on different ones:\n%s",
			s.treeTopology, s.leafRegistrationReport(ctx, s.treeTopology, partition))
	}

	cross, same := pickLeafPairs(leaves, nodesByLeaf, s.treeTopology, partition, s.nodeIsIdle)
	if cross == nil {
		s.runtime.Logf("acceptance: no two idle workers of topology %s sit on different leaf switches right now, skipping the switch limit check:\n%s",
			s.treeTopology, s.leafRegistrationReport(ctx, s.treeTopology, partition))
		return godog.ErrSkip
	}

	s.crossLeaf, s.sameLeaf = cross, same
	s.runtime.Logf("acceptance: workers %s and %s of partition %s hang off leaf switches %s and %s of topology %s",
		cross.Nodes[0], cross.Nodes[1], partition, cross.Leaves[0], cross.Leaves[1], s.treeTopology)
	return nil
}

func (s *Topology) nodeIsIdle(node string) bool {
	view, ok := s.nodes[node]
	return ok && view.Idle()
}

// workerStateReport reads the named workers back from the cluster, so a failure reports the state
// they are in now rather than the state that made them look allocatable earlier.
func (s *Topology) workerStateReport(ctx context.Context, nodes []string) string {
	s.nodes = nil
	if err := s.loadNodes(ctx); err != nil {
		return fmt.Sprintf("  re-read the workers: %v", err)
	}

	lines := make([]string, 0, len(nodes))
	for _, node := range nodes {
		view, ok := s.nodes[node]
		if !ok {
			lines = append(lines, fmt.Sprintf("  %s is missing from scontrol show node", node))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s state=%s reason=%q", node, view.Info.State, view.Info.Reason))
	}
	return strings.Join(lines, "\n")
}

// leafRegistrationReport describes what the partition's workers are doing, so the failure above
// names the states that made the pair unavailable instead of only reporting its absence.
func (s *Topology) leafRegistrationReport(ctx context.Context, topology, partition string) string {
	nodes, err := s.partitionNodeSet(ctx, partition)
	if err != nil {
		return fmt.Sprintf("  read the nodes of partition %s: %v", partition, err)
	}

	var lines []string
	for node := range nodes {
		view, ok := s.nodes[node]
		if !ok {
			lines = append(lines, fmt.Sprintf("  %s is missing from scontrol show node", node))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s state=%s leaf=%s",
			node, view.Info.State, registeredLeaf(view.Units[topology])))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func (s *Topology) requestCrossLeafWorkers(ctx context.Context) error {
	if err := s.pickWorkersOnDifferentLeaves(ctx); err != nil {
		return err
	}
	pair := *s.crossLeaf

	// The unconstrained run tells a topology constraint apart from workers that are merely busy.
	out, err := s.srun(ctx, srunRequest{
		Partition: pair.Partition,
		Nodes:     pair.Nodes[:],
		Immediate: topologyJobImmediate,
	})
	s.crossLeafRun = allocationResult{Output: out, Err: err, Requested: true}
	return nil
}

func (s *Topology) crossLeafAllocationSucceeds(ctx context.Context) error {
	run, err := s.requestedAllocation()
	if err != nil {
		return err
	}
	if run.Err == nil {
		return nil
	}
	pair := *s.crossLeaf
	if srunRefused(run.Output, run.Err) {
		// The pair was idle when it was picked, so a refusal here is not the busy cluster the pick
		// already skips on. The states are printed because the useful answer is which of the two
		// workers stopped being allocatable, and why.
		return fmt.Errorf("workers %s and %s were idle when they were picked, but Slurm allocated neither within %s:\n%s",
			pair.Nodes[0], pair.Nodes[1], topologyJobImmediate, s.workerStateReport(ctx, pair.Nodes[:]))
	}
	return fmt.Errorf("run the unconstrained job on %s: %w", strings.Join(pair.Nodes[:], ","), run.Err)
}

func (s *Topology) requestCrossLeafWorkersWithinOneSwitch(ctx context.Context) error {
	if _, err := s.requestedAllocation(); err != nil {
		return err
	}
	pair := *s.crossLeaf

	out, srunErr := s.srun(ctx, srunRequest{
		Partition: pair.Partition,
		Nodes:     pair.Nodes[:],
		Switches:  1,
		Immediate: topologyDenyImmediate,
	})
	s.crossLeafRun = allocationResult{Output: out, Err: srunErr, Requested: true}
	return nil
}

// crossLeafAllocationIsRejected is the behavioural half of the tree check. The rendered file and the
// registrations can both look right while the scheduler ignores the tree, and multi-topology
// clusters no longer expose the switch path through $SLURM_TOPOLOGY_ADDR - it reports the bare node
// name - so the only remaining proof is that --switches actually constrains a placement.
func (s *Topology) crossLeafAllocationIsRejected() error {
	run, err := s.requestedAllocation()
	if err != nil {
		return err
	}
	pair := *s.crossLeaf

	if run.Err == nil {
		return fmt.Errorf("topology %s did not constrain placement: %s (leaf %s) and %s (leaf %s) were allocated within one switch:\n%s",
			pair.Topology, pair.Nodes[0], pair.Leaves[0], pair.Nodes[1], pair.Leaves[1], strings.TrimSpace(run.Output))
	}
	if !srunRefused(run.Output, run.Err) {
		return fmt.Errorf("the switch-limited job on %s failed for an unrelated reason: %w",
			strings.Join(pair.Nodes[:], ","), run.Err)
	}
	return nil
}

// sameLeafAllocationSucceeds is the positive control of the rejection above: the same switch limit
// has to allow a pair that does sit on one leaf, otherwise the rejection proves nothing.
func (s *Topology) sameLeafAllocationSucceeds(ctx context.Context) error {
	if s.crossLeaf == nil {
		return fmt.Errorf("no cross-leaf worker pair was picked")
	}
	if s.sameLeaf == nil {
		s.runtime.Logf("acceptance: no leaf switch of topology %s holds two running workers, skipping the allowed-placement half",
			s.crossLeaf.Topology)
		return nil
	}

	same := *s.sameLeaf
	if _, err := s.srun(ctx, srunRequest{
		Partition: same.Partition,
		Nodes:     same.Nodes[:],
		Switches:  1,
		Immediate: topologyJobImmediate,
	}); err != nil {
		return fmt.Errorf("run a one-switch job on %s and %s, both on leaf switch %s: %w",
			same.Nodes[0], same.Nodes[1], same.Leaves[0], err)
	}
	return nil
}

func (s *Topology) requestedAllocation() (allocationResult, error) {
	if !s.crossLeafRun.Requested {
		return allocationResult{}, fmt.Errorf("no allocation was requested")
	}
	if s.crossLeaf == nil {
		return allocationResult{}, fmt.Errorf("no cross-leaf worker pair was picked")
	}
	return s.crossLeafRun, nil
}

// collectLeafPairs reads the leaf switches of one tree topology back from slurmctld and records a
// pair of workers on different leaves, plus a pair on the same leaf when the tree has one.
// collectLeafMembership maps every leaf switch of the topology to the partition's workers that
// Slurm says hang off it. Only workers running slurmd are listed: a powered-down one carries no
// registration and could not be placed anyway.
func (s *Topology) collectLeafMembership(
	ctx context.Context, topology, partition string,
) ([]string, map[string][]string, error) {
	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol show topology %s", framework.ShellQuote(topology)))
	if err != nil {
		return nil, nil, fmt.Errorf("scontrol show topology %s: %w", topology, err)
	}
	switches, err := parseSlurmSwitches(out)
	if err != nil {
		return nil, nil, fmt.Errorf("parse scontrol show topology %s: %w", topology, err)
	}

	partitionNodes, err := s.partitionNodeSet(ctx, partition)
	if err != nil {
		return nil, nil, err
	}

	var leaves []string
	nodesByLeaf := make(map[string][]string)
	for _, sw := range switches {
		if !sw.IsLeaf() {
			continue
		}
		nodes, err := s.expandHostlist(ctx, sw.Nodes)
		if err != nil {
			return nil, nil, err
		}
		for _, node := range nodes {
			if _, ok := partitionNodes[node]; !ok {
				continue
			}
			view, ok := s.nodes[node]
			if !ok || !view.Ready() {
				continue
			}
			// The node must agree it hangs off this switch: the pair is only meaningful if the
			// running cluster, not just the rendered file, separates the two workers.
			if registeredLeaf(view.Units[topology]) != sw.Name {
				continue
			}
			if _, seen := nodesByLeaf[sw.Name]; !seen {
				leaves = append(leaves, sw.Name)
			}
			nodesByLeaf[sw.Name] = append(nodesByLeaf[sw.Name], node)
		}
	}
	sort.Strings(leaves)
	return leaves, nodesByLeaf, nil
}

// pickLeafPairs picks one pair of workers on different leaf switches and one pair sharing a leaf,
// out of the workers keep accepts. Either pair may be absent.
func pickLeafPairs(
	leaves []string, nodesByLeaf map[string][]string, topology, partition string, keep func(string) bool,
) (*leafPair, *leafPair) {
	var (
		kept      []string
		keptNodes = make(map[string][]string, len(nodesByLeaf))
		same      *leafPair
	)
	for _, leaf := range leaves {
		var nodes []string
		for _, node := range nodesByLeaf[leaf] {
			if keep(node) {
				nodes = append(nodes, node)
			}
		}
		if len(nodes) == 0 {
			continue
		}
		kept = append(kept, leaf)
		keptNodes[leaf] = nodes
		if same == nil && len(nodes) >= 2 {
			same = &leafPair{
				Partition: partition,
				Topology:  topology,
				Nodes:     [2]string{nodes[0], nodes[1]},
				Leaves:    [2]string{leaf, leaf},
			}
		}
	}
	if len(kept) < 2 {
		return nil, same
	}
	cross := &leafPair{
		Partition: partition,
		Topology:  topology,
		Nodes:     [2]string{keptNodes[kept[0]][0], keptNodes[kept[1]][0]},
		Leaves:    [2]string{kept[0], kept[1]},
	}
	return cross, same
}

// region block topology

func (s *Topology) slurmReportsRenderedBlocks(ctx context.Context) error {
	entry, ok := topologyByName(s.rendered, s.blockTopology)
	if !ok {
		return fmt.Errorf("topology %q disappeared from the rendered config", s.blockTopology)
	}
	blocks, err := s.slurmBlocksOf(ctx, s.blockTopology)
	if err != nil {
		return err
	}

	rendered := make(map[string]string, len(entry.Blocks))
	for _, block := range entry.Blocks {
		rendered[block.Name] = block.Nodes
	}
	if len(blocks) != len(rendered) {
		return fmt.Errorf("slurmctld reports %d blocks of topology %s, the operator rendered %d",
			len(blocks), s.blockTopology, len(rendered))
	}

	base := 0
	if len(entry.BlockSizes) > 0 {
		base = entry.BlockSizes[0]
	}
	for _, block := range blocks {
		nodes, ok := rendered[block.Name]
		if !ok {
			return fmt.Errorf("slurmctld reports block %s of topology %s, the operator rendered none",
				block.Name, s.blockTopology)
		}
		if nodes != block.Nodes {
			return fmt.Errorf("block %s holds %s in slurmctld and %s in the rendered config",
				block.Name, block.Nodes, nodes)
		}
		if base > 0 && block.Size != base {
			return fmt.Errorf("block %s has BlockSize=%d, the rendered config asks for a base size of %d",
				block.Name, block.Size, base)
		}
	}
	return nil
}

func (s *Topology) jobRunsInBlockPartition(ctx context.Context) error {
	if err := s.loadPartitions(ctx); err != nil {
		return err
	}
	if err := s.loadNodes(ctx); err != nil {
		return err
	}
	return s.runJobInTopology(ctx, s.blockTopology)
}

func (s *Topology) halveBlockTopologySize(ctx context.Context) error {
	cluster, err := s.readSlurmCluster(ctx)
	if err != nil {
		return err
	}
	index, spec, ok := namedTopologyByName(cluster, s.blockTopology)
	if !ok {
		return fmt.Errorf("SlurmCluster %s declares no topology %q", s.info.SlurmClusterName, s.blockTopology)
	}

	sizes, ok := halveBlockSizes(spec.Topo.BlockSizes)
	if !ok {
		return fmt.Errorf("topology %s has blockSizes %v, which cannot be halved", s.blockTopology, spec.Topo.BlockSizes)
	}

	s.blockSizesRestore = spec.Topo.BlockSizes
	s.runtime.Logf("acceptance: changing blockSizes of topology %s from %v to %v",
		s.blockTopology, spec.Topo.BlockSizes, sizes)
	if err := s.patchBlockSizesAt(ctx, index, sizes); err != nil {
		return err
	}
	return nil
}

// renderedConfigCarriesBlockSizes waits for the ConfigMap and the JailedConfig annotation together
// rather than one after the other. The operator publishes the content before recording the
// structure, on purpose, so a config that already carries the new sizes and an annotation that
// still records the old ones is a valid intermediate state, not a disagreement.
func (s *Topology) renderedConfigCarriesBlockSizes(ctx context.Context) error {
	sizes, ok := halveBlockSizes(s.blockSizesRestore)
	if !ok {
		return fmt.Errorf("no blockSizes change was applied")
	}

	var converged []topologyEntry
	err := s.runtime.WaitFor(ctx,
		fmt.Sprintf("the published config and the JailedConfig to carry blockSizes %v of topology %s",
			sizes, s.blockTopology),
		topologyConvergeTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			_, rendered, err := s.readTopologyConfigMap(waitCtx)
			if err != nil {
				return false, err
			}
			entries, err := parseTopologyEntries(rendered)
			if err != nil {
				return false, err
			}
			entry, ok := topologyByName(entries, s.blockTopology)
			if !ok {
				return false, fmt.Errorf("topology %s is missing from the published config", s.blockTopology)
			}
			if !sameIntSlice(entry.BlockSizes, sizes) {
				return false, fmt.Errorf("topology %s still carries blockSizes %v", s.blockTopology, entry.BlockSizes)
			}

			structure, err := s.jailedConfigStructure(waitCtx)
			if err != nil {
				return false, err
			}
			if want := topologyStructure(entries); structure != want {
				return false, fmt.Errorf("the JailedConfig records structure %q, the published config is %q",
					structure, want)
			}
			converged = entries
			return true, nil
		})
	if err != nil {
		return err
	}
	s.rendered = converged
	return nil
}

func (s *Topology) slurmReportsBaseBlockSize(ctx context.Context) error {
	sizes, ok := halveBlockSizes(s.blockSizesRestore)
	if !ok {
		return fmt.Errorf("no blockSizes change was applied")
	}

	return s.waitForSlurmBaseBlockSize(ctx, s.blockTopology, sizes[0])
}

func (s *Topology) waitForSlurmBaseBlockSize(ctx context.Context, topology string, base int) error {
	return s.runtime.WaitFor(ctx,
		fmt.Sprintf("slurmctld to report BlockSize=%d for topology %s", base, topology),
		topologyConvergeTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			blocks, err := s.slurmBlocksOf(waitCtx, topology)
			if err != nil {
				return false, err
			}
			for _, block := range blocks {
				if block.Size != base {
					return false, fmt.Errorf("block %s still has BlockSize=%d", block.Name, block.Size)
				}
			}
			return true, nil
		})
}

// noPendingReconfigureRequest waits for the request to be withdrawn rather than reading it once.
// Withdrawal trails the reconfigure it asked for: the operator only drops the action after seeing
// the JailedConfig's applied hash move past the value it recorded when raising it, which takes a
// further reconciliation after slurmctld has already picked the new config up.
func (s *Topology) noPendingReconfigureRequest(ctx context.Context) error {
	return s.runtime.WaitFor(ctx,
		fmt.Sprintf("JailedConfig %s to withdraw its reconfigure request", s.configName),
		topologyConvergeTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			config, err := s.readJailedConfig(waitCtx)
			if err != nil {
				return false, err
			}
			if len(config.Spec.UpdateActions) > 0 {
				return false, fmt.Errorf("JailedConfig %s still asks for %s",
					s.configName, strings.Join(config.Spec.UpdateActions, ", "))
			}
			return true, nil
		})
}

func (s *Topology) readJailedConfig(ctx context.Context) (kubeobjects.JailedConfig, error) {
	var config kubeobjects.JailedConfig
	if err := s.kubectl.GetJSON(ctx, &config,
		"get", "jailedconfig", s.configName, "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return kubeobjects.JailedConfig{}, fmt.Errorf("get JailedConfig %s: %w", s.configName, err)
	}
	return config, nil
}

func (s *Topology) waitForRenderedBlockSizes(ctx context.Context, topology string, sizes []int) error {
	return s.runtime.WaitFor(ctx,
		fmt.Sprintf("the published config to carry blockSizes %v of topology %s", sizes, topology),
		topologyConvergeTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			_, rendered, err := s.readTopologyConfigMap(waitCtx)
			if err != nil {
				return false, err
			}
			entries, err := parseTopologyEntries(rendered)
			if err != nil {
				return false, err
			}
			entry, ok := topologyByName(entries, topology)
			if !ok {
				return false, fmt.Errorf("topology %s is missing from the published config", topology)
			}
			if !sameIntSlice(entry.BlockSizes, sizes) {
				return false, fmt.Errorf("topology %s still carries blockSizes %v", topology, entry.BlockSizes)
			}
			return true, nil
		})
}

func (s *Topology) patchBlockSizes(ctx context.Context, topology string, sizes []int) error {
	cluster, err := s.readSlurmCluster(ctx)
	if err != nil {
		return err
	}
	index, _, ok := namedTopologyByName(cluster, topology)
	if !ok {
		return fmt.Errorf("SlurmCluster %s declares no topology %q", s.info.SlurmClusterName, topology)
	}
	return s.patchBlockSizesAt(ctx, index, sizes)
}

func (s *Topology) patchBlockSizesAt(ctx context.Context, index int, sizes []int) error {
	value, err := json.Marshal(sizes)
	if err != nil {
		return fmt.Errorf("marshal blockSizes %v: %w", sizes, err)
	}
	patch := fmt.Sprintf(`[{"op":"replace","path":"/spec/topology/topologies/%d/topo/blockSizes","value":%s}]`,
		index, value)

	if _, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
		"patch", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace,
		"--type=json", "-p", patch,
	); err != nil {
		return fmt.Errorf("patch blockSizes of SlurmCluster %s: %w", s.info.SlurmClusterName, err)
	}
	return nil
}

func (s *Topology) jailedConfigStructure(ctx context.Context) (string, error) {
	config, err := s.readJailedConfig(ctx)
	if err != nil {
		return "", err
	}
	return config.Metadata.Annotations[topologyStructureKey], nil
}

func (s *Topology) readSlurmCluster(ctx context.Context) (kubeobjects.SlurmCluster, error) {
	var cluster kubeobjects.SlurmCluster
	if err := s.kubectl.GetJSON(ctx, &cluster,
		"get", "slurmcluster", s.info.SlurmClusterName, "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return kubeobjects.SlurmCluster{}, fmt.Errorf("get SlurmCluster %s: %w", s.info.SlurmClusterName, err)
	}
	return cluster, nil
}

func namedTopologyByName(cluster kubeobjects.SlurmCluster, name string) (int, kubeobjects.NamedTopology, bool) {
	if cluster.Spec.Topology == nil {
		return 0, kubeobjects.NamedTopology{}, false
	}
	for i, topology := range cluster.Spec.Topology.Topologies {
		if topology.Name == name {
			return i, topology, true
		}
	}
	return 0, kubeobjects.NamedTopology{}, false
}

// region cluster state

func (s *Topology) loadWorkers(ctx context.Context) error {
	if len(s.snapshot.Workers) > 0 {
		return nil
	}
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	s.snapshot = snapshot
	return nil
}

func (s *Topology) loadNodes(ctx context.Context) error {
	if s.nodes != nil {
		return nil
	}
	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show node")
	if err != nil {
		return fmt.Errorf("scontrol show node: %w", err)
	}

	nodes := make(map[string]slurmNodeView)
	for name, block := range parseScontrolBlocks(out, "NodeName") {
		nodes[name] = slurmNodeView{
			Info:  framework.ParseSlurmNodeInfo(name, block),
			Units: parseNodeTopologies(block),
		}
	}
	if len(nodes) == 0 {
		return fmt.Errorf("scontrol show node reported no node")
	}
	s.nodes = nodes
	return nil
}

func (s *Topology) loadPartitions(ctx context.Context) error {
	if s.partitions != nil {
		return nil
	}
	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show partition")
	if err != nil {
		return fmt.Errorf("scontrol show partition: %w", err)
	}
	partitions := parsePartitions(out)
	if len(partitions) == 0 {
		return fmt.Errorf("scontrol show partition reported no partition")
	}
	s.partitions = partitions
	return nil
}

func (s *Topology) partitionOfTopology(topology string) (string, bool) {
	for _, partition := range s.partitions {
		if partition.Topology == topology {
			return partition.Name, true
		}
	}
	return "", false
}

func (s *Topology) partitionNodeSet(ctx context.Context, name string) (map[string]struct{}, error) {
	for _, partition := range s.partitions {
		if partition.Name != name {
			continue
		}
		nodes, err := s.expandHostlist(ctx, partition.Nodes)
		if err != nil {
			return nil, err
		}
		set := make(map[string]struct{}, len(nodes))
		for _, node := range nodes {
			set[node] = struct{}{}
		}
		return set, nil
	}
	return nil, fmt.Errorf("partition %q is unknown to Slurm", name)
}

func (s *Topology) topologyNodes(ctx context.Context, entry topologyEntry) ([]string, error) {
	var lists []string
	switch entry.Kind {
	case topologyKindTree:
		for _, sw := range entry.Switches {
			lists = append(lists, sw.Nodes)
		}
	case topologyKindBlock:
		for _, block := range entry.Blocks {
			lists = append(lists, block.Nodes)
		}
	default:
		return nil, nil
	}

	var nodes []string
	for _, list := range lists {
		expanded, err := s.expandHostlist(ctx, list)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, expanded...)
	}
	return nodes, nil
}

func (s *Topology) slurmBlocksOf(ctx context.Context, topology string) ([]slurmBlock, error) {
	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol show topology %s", framework.ShellQuote(topology)))
	if err != nil {
		return nil, fmt.Errorf("scontrol show topology %s: %w", topology, err)
	}
	blocks, err := parseSlurmBlocks(out)
	if err != nil {
		// scontrol answers an unknown topology on stdout and still exits 0, so the parse failure is
		// where a missing topology surfaces.
		return nil, fmt.Errorf("parse scontrol show topology %s: %w", topology, err)
	}
	return blocks, nil
}

// expandHostlist turns a Slurm hostlist into node names, asking scontrol only for the values that
// carry a range.
func (s *Topology) expandHostlist(ctx context.Context, value string) ([]string, error) {
	if nodes, ok := plainHostlist(value); ok {
		return nodes, nil
	}
	if cached, ok := s.hostlists[value]; ok {
		return cached, nil
	}

	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol show hostnames %s", framework.ShellQuote(value)))
	if err != nil {
		return nil, fmt.Errorf("expand hostlist %q: %w", value, err)
	}
	nodes := framework.ParseSlurmNodeNames(out)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("hostlist %q expanded to nothing", value)
	}

	if s.hostlists == nil {
		s.hostlists = make(map[string][]string)
	}
	s.hostlists[value] = nodes
	return nodes, nil
}

// region srun

type srunRequest struct {
	Partition string
	Nodes     []string
	// Switches caps the number of leaf switches the allocation may span. Zero leaves the job
	// unconstrained.
	Switches  int
	Immediate time.Duration
}

func (r srunRequest) command() string {
	args := []string{
		fmt.Sprintf("--immediate=%d", int(r.Immediate.Seconds())),
		fmt.Sprintf("-p %s", framework.ShellQuote(r.Partition)),
		fmt.Sprintf("-N %d", len(r.Nodes)),
		fmt.Sprintf("-n %d", len(r.Nodes)),
		"--ntasks-per-node=1",
		fmt.Sprintf("-w %s", framework.ShellQuote(strings.Join(r.Nodes, ","))),
		"--time=1:00",
	}
	if r.Switches > 0 {
		args = append(args, fmt.Sprintf("--switches=%d", r.Switches))
	}

	// The outer timeout is a backstop for an srun that neither runs nor gives up; --immediate is
	// what normally ends the wait.
	return fmt.Sprintf("timeout %d srun %s hostname",
		int(r.Immediate.Seconds())+60, strings.Join(args, " "))
}

func (s *Topology) srun(ctx context.Context, request srunRequest) (string, error) {
	command := request.command()
	// Never retried: a scheduling decision is the subject of the check, not a flaky call.
	out, err := s.runtime.Jail().Run(ctx, command)
	s.runtime.Logf("acceptance: %s\n%s", command, strings.TrimSpace(out))
	return out, err
}

func sameStringSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

func sameIntSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
