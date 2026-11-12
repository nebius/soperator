package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	// vmStopStartReservationPrefix names the scenario's reservations, so a later run finds and deletes stale ones.
	vmStopStartReservationPrefix = "e2e-vm-stop-start-"

	// vmStopStartReservationDuration releases the workers even if cleanup is lost.
	vmStopStartReservationDuration = 60 * time.Minute

	// vmStopStartIdleTimeout lets jobs already running on the reserved workers finish.
	vmStopStartIdleTimeout = 10 * time.Minute

	// vmStopStartInstanceTimeout bounds a Compute stop or start until the instance state; both take 2-3 minutes.
	vmStopStartInstanceTimeout = 10 * time.Minute

	// vmStopStartNotRespondingTimeout covers SlurmdTimeout (180s by default) and the ping cycle.
	vmStopStartNotRespondingTimeout = 10 * time.Minute

	// vmStopStartRecoveryTimeout covers the VM boot, the worker pod restart and slurmd registration; it takes 4-5 minutes.
	vmStopStartRecoveryTimeout = 20 * time.Minute

	// vmStopStartTopologyTimeout lets slurmctld reload the topology after the workers register again.
	vmStopStartTopologyTimeout = 5 * time.Minute

	// vmStopStartJobTimeout bounds a whole check job, allocation included.
	vmStopStartJobTimeout = 5 * time.Minute

	// vmStopStartJobImmediate is how long a check job waits for its allocation before it fails.
	vmStopStartJobImmediate = 2 * time.Minute

	// vmStopStartCleanupTimeout bounds the cleanup wait for stopped workers to become usable again.
	vmStopStartCleanupTimeout = 15 * time.Minute

	// vmStopStartReleaseTimeout bounds each quick cleanup call: resuming the drain and deleting the reservation.
	vmStopStartReleaseTimeout = 2 * time.Minute

	// vmStopStartDrainReason carries no Soperator-owned reason prefix, so no controller or check may clear it.
	// It is the same in every run: drain reasons become metric labels, and a unique reason per run would
	// grow their cardinality.
	vmStopStartDrainReason = "e2e vm stop-start manual drain"

	// vmStopStartNodeKey prefixes the line each task of the job across all workers prints with its node name.
	vmStopStartNodeKey = "vm_stop_start_node"

	// vmStopStartGPUsKey prefixes the line each task of the GPU job prints with its node name and GPU count.
	vmStopStartGPUsKey = "vm_stop_start_gpus"
)

// VMStopStart stops and starts the Compute instances of workers and checks that
// the cluster recovers them without manual intervention.
type VMStopStart struct {
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	reservation string
	// workers keep the Slurm node state recorded at selection.
	workers []framework.WorkerInfo
	// topologies lists, by worker, the topologies slurmctld placed it in before the stop-start.
	topologies map[string][]string
	// instancesStopped records that the scenario requested to stop the reserved instances; cleanup starts them again.
	instancesStopped bool
	// operations are the Compute operations requested last; cleanup waits for them before it looks at instance states.
	operations []string
	// drainedWorker is the worker drained by hand before the stop-start; cleanup resumes it.
	drainedWorker string
}

func NewVMStopStart(
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *VMStopStart {
	return &VMStopStart{runtime: runtime, slurm: slurm, kubectl: kubectl, selector: selector}
}

func (s *VMStopStart) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^up to (\d+) workers of every NodeSet are reserved for a VM stop-start$`, s.reserveWorkers)
	sc.Step(`^the reserved workers are idle$`, s.reservedWorkersAreIdle)
	sc.Step(`^an administrator drains one reserved worker$`, s.drainOneWorker)
	sc.Step(`^the VMs of the reserved workers are stopped$`, s.stopVMs)
	sc.Step(`^Slurm reports the reserved workers as not responding$`, s.reservedWorkersNotResponding)
	sc.Step(`^the Slurm controller answers cluster queries$`, s.controllerAnswers)
	sc.Step(`^the VMs of the reserved workers are started$`, s.startVMs)
	sc.Step(`^the reserved workers return to service without manual intervention$`, s.reservedWorkersReturnToService)
	sc.Step(`^the drained worker keeps its drain$`, s.drainedWorkerKeepsDrain)
	sc.Step(`^the administrator resumes the drained worker$`, s.resumeDrainedWorker)
	sc.Step(`^the reserved workers rejoin their topologies$`, s.reservedWorkersRejoinTopologies)
	sc.Step(`^a job runs across all reserved workers$`, s.jobRunsAcrossReservedWorkers)
	sc.Step(`^a job sees every GPU of each reserved GPU worker$`, s.jobSeesEveryGPUOfReservedGPUWorkers)
	sc.Step(`^node-local scratch is mounted on a dedicated filesystem on every reserved worker that has it$`, s.localScratchIsMounted)
}

// CleanupAndReset starts every instance the scenario stopped, resumes its own drain,
// waits for the workers to become usable again, and releases the reservation.
func (s *VMStopStart) CleanupAndReset(ctx context.Context) {
	if s.instancesStopped {
		ids := s.instanceIDs()
		runCleanupPhase(ctx, s.runtime, "start stopped Compute instances "+strings.Join(ids, ","), 2*vmStopStartInstanceTimeout, func(ctx context.Context) error {
			// A stop requested just before a failure may not show yet: an instance still reported RUNNING
			// would be left alone and stop afterwards. A failed operation changes nothing, so its error only matters as a log.
			if err := waitForInstanceOperations(ctx, s.runtime, s.operations, vmStopStartInstanceTimeout); err != nil {
				s.runtime.Logf("Cleanup: wait for Compute operations %s: %v", strings.Join(s.operations, ","), err)
			}
			return startStoppedInstances(ctx, s.runtime, ids, vmStopStartInstanceTimeout)
		})
	}
	// A drained worker never becomes usable, so the drain goes before the wait below.
	if s.drainedWorker != "" {
		runCleanupPhase(ctx, s.runtime, "resume manually drained worker "+s.drainedWorker, vmStopStartReleaseTimeout, func(ctx context.Context) error {
			return s.slurm.ResumeNodeIfDrainedByReason(ctx, s.drainedWorker, vmStopStartDrainReason)
		})
	}
	if s.instancesStopped {
		runCleanupPhase(ctx, s.runtime, "wait for reserved workers "+strings.Join(s.workerNames(), ","), vmStopStartCleanupTimeout, func(ctx context.Context) error {
			return s.waitForWorkers(ctx, "reserved workers usable after cleanup", vmStopStartCleanupTimeout, framework.SlurmNodeInfo.IsUsable)
		})
	}
	if s.reservation != "" {
		runCleanupPhase(ctx, s.runtime, "delete reservation "+s.reservation, vmStopStartReleaseTimeout, func(ctx context.Context) error {
			_, err := s.runtime.Jail().RunWithDefaultRetry(ctx, deleteReservationCommand(s.reservation))
			return err
		})
	}
	*s = VMStopStart{runtime: s.runtime, slurm: s.slurm, kubectl: s.kubectl, selector: s.selector}
}

func (s *VMStopStart) reserveWorkers(ctx context.Context, perNodeSet int) error {
	if err := deleteStaleReservations(ctx, s.runtime, vmStopStartReservationPrefix); err != nil {
		return err
	}
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	selected, skipped, err := selectVMStopStartWorkers(snapshot, perNodeSet)
	for _, note := range skipped {
		s.runtime.Logf("VM stop-start: %s", note)
	}
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		s.runtime.Logf("VM stop-start: no static NodeSet has workers, skipping scenario")
		return godog.ErrSkip
	}

	names := framework.WorkerNames(selected)
	name := fmt.Sprintf("%s%d", vmStopStartReservationPrefix, time.Now().UnixNano())
	// Record ownership before the create: a lost response may still have created it.
	s.reservation = name
	// The show after the create decides success, so a retry after a lost exec
	// stream finds the reservation already created instead of failing.
	command := nodeReservationCommand(name, "root", names, vmStopStartReservationDuration, "IGNORE_JOBS") +
		"; scontrol show reservation " + framework.ShellQuote(name) + " --oneliner"
	shown, err := s.runtime.Jail().RunWithDefaultRetry(ctx, command)
	if err != nil {
		return fmt.Errorf("reserve workers %s: %w", strings.Join(names, ","), err)
	}
	if nodes := extractField(reservationsByName(shown)[name], "Nodes"); nodes == "" {
		return fmt.Errorf("reservation %s for workers %s was not created: %s", name, strings.Join(names, ","), strings.TrimSpace(shown))
	}

	s.workers = selected
	placements, err := s.readTopologyPlacements(ctx, newCachedHostlistExpander(s.runtime), s.runtime.Controller().RunWithDefaultRetry)
	if err != nil {
		return err
	}
	s.topologies = make(map[string][]string, len(selected))
	var unknown []string
	for _, worker := range selected {
		units := placements[worker.Name]
		s.topologies[worker.Name] = slices.Sorted(maps.Keys(units))
		for _, topology := range s.topologies[worker.Name] {
			if isUnknownTopologyUnit(units[topology]) {
				unknown = append(unknown, fmt.Sprintf("%s under %s/%s", worker.Name, topology, units[topology]))
			}
		}
		s.runtime.Logf("VM stop-start: reserved worker %s of NodeSet %s on Compute instance %s, placed in %v",
			worker.Name, worker.NodeSetName, worker.SlurmNode.InstanceID, units)
	}
	if len(unknown) > 0 {
		return fmt.Errorf("reserved workers are already outside their topologies before the stop-start: %s", strings.Join(unknown, "; "))
	}
	s.runtime.Logf("VM stop-start: reservation %s holds %s for %s", name, strings.Join(names, ","), vmStopStartReservationDuration)
	return nil
}

// selectVMStopStartWorkers picks up to perNodeSet usable workers of every static NodeSet with replicas,
// idle ones first, and fails when a static NodeSet cannot offer min(perNodeSet, replicas) of them.
// Ephemeral NodeSets are skipped: power saving would turn their idle workers off during the stop-start.
func selectVMStopStartWorkers(snapshot framework.WorkerSnapshot, perNodeSet int) (selected []framework.WorkerInfo, skipped []string, err error) {
	if perNodeSet < 1 {
		return nil, nil, fmt.Errorf("invalid worker count per NodeSet %d", perNodeSet)
	}
	var problems []error
	for _, nodeSet := range snapshot.NodeSets {
		if nodeSet.Replicas <= 0 {
			continue
		}
		if nodeSet.Ephemeral {
			skipped = append(skipped, fmt.Sprintf("skipping ephemeral NodeSet %s", nodeSet.Name))
			continue
		}
		pool := snapshot.WorkersByNodeSet[nodeSet.Name]
		var candidates []framework.WorkerInfo
		for _, worker := range pool {
			if worker.IsUsable() {
				candidates = append(candidates, worker)
			}
		}
		slices.SortStableFunc(candidates, func(a, b framework.WorkerInfo) int {
			if a.IsIdle() != b.IsIdle() {
				if a.IsIdle() {
					return -1
				}
				return 1
			}
			return strings.Compare(a.Name, b.Name)
		})

		want := min(perNodeSet, nodeSet.Replicas)
		if len(candidates) < want {
			problems = append(problems, errors.New(framework.WorkerAvailability(pool, nodeSet.Replicas, want,
				"workers of NodeSet "+nodeSet.Name, framework.WorkerInfo.IsUsable)))
			continue
		}
		for _, worker := range candidates[:want] {
			if worker.SlurmNode.InstanceID == "" {
				problems = append(problems, fmt.Errorf("worker %s of NodeSet %s has no InstanceId: state=%s reason=%q",
					worker.Name, nodeSet.Name, worker.SlurmNode.State, worker.SlurmNode.Reason))
				continue
			}
			selected = append(selected, worker)
		}
	}
	if err := errors.Join(problems...); err != nil {
		return nil, skipped, fmt.Errorf("select workers for a VM stop-start: %w", err)
	}
	if len(selected) == 0 && len(skipped) == 0 {
		return nil, nil, fmt.Errorf("select workers for a VM stop-start: no NodeSet has workers")
	}
	return selected, skipped, nil
}

func (s *VMStopStart) reservedWorkersAreIdle(ctx context.Context) error {
	return s.waitForWorkers(ctx, "reserved workers idle", vmStopStartIdleTimeout, framework.SlurmNodeInfo.IsIdle)
}

func (s *VMStopStart) stopVMs(ctx context.Context) error {
	// Record ownership before the requests: a lost response may still have stopped an instance.
	s.instancesStopped = true
	return s.applyInstanceAction(ctx, "stop", instanceStateStopped)
}

func (s *VMStopStart) reservedWorkersNotResponding(ctx context.Context) error {
	return s.waitForWorkers(ctx, "Slurm to report the stopped workers as not responding",
		vmStopStartNotRespondingTimeout, framework.SlurmNodeInfo.IsUnreachable)
}

// controllerAnswers queries the controller from the login node, as a user would, with no retries:
// a controller that only answers after a retry is not answering.
func (s *VMStopStart) controllerAnswers(ctx context.Context) error {
	names := s.workerNames()
	output, err := s.runtime.Jail().Run(ctx, partitionNodesCommand(framework.MainPartitionName))
	if err != nil {
		return fmt.Errorf("query node states with sinfo while workers %s are stopped: %w", strings.Join(names, ","), err)
	}
	nodes, err := parsePartitionNodes(output)
	if err != nil {
		return err
	}
	states := make(map[string]string, len(nodes))
	for _, node := range nodes {
		states[node.Name] = node.State
	}
	var reported, missing []string
	for _, name := range names {
		state, found := states[name]
		if !found {
			missing = append(missing, name)
			continue
		}
		reported = append(reported, name+"="+state)
	}
	if len(missing) > 0 {
		return fmt.Errorf("sinfo does not list stopped workers %s: %s", strings.Join(missing, ","), strings.TrimSpace(output))
	}
	if _, err := s.runtime.Jail().Run(ctx, "squeue -h -o '%i' >/dev/null"); err != nil {
		return fmt.Errorf("query the job queue with squeue while workers %s are stopped: %w", strings.Join(names, ","), err)
	}
	s.runtime.Logf("VM stop-start: the controller answers with stopped workers %s", strings.Join(reported, ", "))
	return nil
}

func (s *VMStopStart) startVMs(ctx context.Context) error {
	if err := s.applyInstanceAction(ctx, "start", instanceStateRunning); err != nil {
		return err
	}
	s.instancesStopped = false
	return nil
}

// applyInstanceAction requests the action on every reserved instance, waits for the Compute operations,
// which report a failure such as missing capacity as soon as it happens, and then confirms the instance states.
func (s *VMStopStart) applyInstanceAction(ctx context.Context, action, state string) error {
	ids := s.instanceIDs()
	s.operations = nil
	for _, id := range ids {
		operation, err := requestInstanceAction(ctx, s.runtime, action, id)
		if err != nil {
			return err
		}
		s.operations = append(s.operations, operation)
	}
	s.runtime.Logf("VM stop-start: requested %s of Compute instances %s, operations %s",
		action, strings.Join(ids, ","), strings.Join(s.operations, ","))
	if err := waitForInstanceOperations(ctx, s.runtime, s.operations, vmStopStartInstanceTimeout); err != nil {
		return err
	}
	return waitForInstancesState(ctx, s.runtime, ids, state, vmStopStartInstanceTimeout, nil)
}

// drainOneWorker drains a random reserved worker the way an administrator would, with a reason no
// Soperator controller or check owns: the drain must survive the stop-start. A random pick covers
// every kind of NodeSet across runs.
func (s *VMStopStart) drainOneWorker(ctx context.Context) error {
	worker := s.workers[rand.IntN(len(s.workers))].Name
	// Record ownership before the update: a lost response may still have drained it.
	s.drainedWorker = worker
	if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, vmStopStartDrainCommand(worker)); err != nil {
		return fmt.Errorf("drain worker %s: %w", worker, err)
	}
	nodes, err := s.slurm.NodeInfosWithRetries(ctx, []string{worker})
	if err != nil {
		return err
	}
	if !nodes[0].IsDrainedFor(vmStopStartDrainReason) {
		return fmt.Errorf("worker %s is not drained after the drain request: state=%s reason=%q", worker, nodes[0].State, nodes[0].Reason)
	}
	s.runtime.Logf("VM stop-start: drained worker %s with reason %q", worker, vmStopStartDrainReason)
	return nil
}

func vmStopStartDrainCommand(worker string) string {
	return framework.ShellJoin("scontrol", "update", "nodename="+worker, "state=DRAIN", "reason="+vmStopStartDrainReason)
}

// reservedWorkersReturnToService never resumes the nodes: Slurm must return them to service by itself.
// The manually drained worker returns as IDLE+DRAIN, every other one as plain idle.
func (s *VMStopStart) reservedWorkersReturnToService(ctx context.Context) error {
	ready := func(node framework.SlurmNodeInfo) bool {
		if node.Name == s.drainedWorker {
			return node.IsIdleIgnoringDrain()
		}
		return node.IsIdle()
	}
	return s.waitForWorkers(ctx, "reserved workers back in service", vmStopStartRecoveryTimeout, ready)
}

func (s *VMStopStart) drainedWorkerKeepsDrain(ctx context.Context) error {
	nodes, err := s.slurm.NodeInfosWithRetries(ctx, []string{s.drainedWorker})
	if err != nil {
		return err
	}
	node := nodes[0]
	if !node.IsDrainedFor(vmStopStartDrainReason) {
		return fmt.Errorf("the stop-start cleared the manual drain of worker %s: state=%s reason=%q, expected DRAIN with reason %q",
			node.Name, node.State, node.Reason, vmStopStartDrainReason)
	}
	s.runtime.Logf("VM stop-start: worker %s kept its manual drain: state=%s", node.Name, node.State)
	return nil
}

func (s *VMStopStart) resumeDrainedWorker(ctx context.Context) error {
	if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, framework.ShellJoin("scontrol", "update", "nodename="+s.drainedWorker, "state=RESUME")); err != nil {
		return fmt.Errorf("resume worker %s: %w", s.drainedWorker, err)
	}
	s.drainedWorker = ""
	return nil
}

// reservedWorkersRejoinTopologies accepts a new placement: a started VM may land on another host
// under other switches. What must hold is that slurmctld places each worker in the same topologies
// as before the stop-start, under a real switch or block rather than an unknown one.
func (s *VMStopStart) reservedWorkersRejoinTopologies(ctx context.Context) error {
	hostlists := newCachedHostlistExpander(s.runtime)
	return s.runtime.WaitFor(ctx, "reserved workers back in their topologies", vmStopStartTopologyTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			placements, err := s.readTopologyPlacements(waitCtx, hostlists, s.runtime.Controller().Run)
			if err != nil {
				return false, err
			}
			if problems := topologyPlacementProblems(s.workerNames(), s.topologies, placements); len(problems) > 0 {
				return false, fmt.Errorf("reserved workers are not back in their topologies: %s", strings.Join(problems, "; "))
			}
			for _, name := range s.workerNames() {
				s.runtime.Logf("VM stop-start: worker %s is placed in %v", name, placements[name])
			}
			return true, nil
		})
}

// readTopologyPlacements reads slurmctld's in-memory topologies, which the scheduler places jobs by,
// and maps each reserved worker, by topology name, to the unit holding it:
// its level-0 switch in a tree, its block in a block topology.
// Flat topologies have no units and are left out. For example:
//
//	placements["worker-0"] == map[string]string{"tree-ib": "leaf-switch-a", "block-nvl72": "block-3"}
//	placements["worker-1"] == map[string]string{"tree-ib": "unknown"}
func (s *VMStopStart) readTopologyPlacements(
	ctx context.Context,
	hostlists *cachedHostlistExpander,
	run func(context.Context, string) (string, error),
) (placements map[string]map[string]string, err error) {
	topologyEntries, _, err := readLoadedTopologyEntries(ctx, s.runtime)
	if err != nil {
		return nil, err
	}
	reserved := make(map[string]bool, len(s.workers))
	for _, worker := range s.workers {
		reserved[worker.Name] = true
	}
	placements = make(map[string]map[string]string, len(s.workers))
	place := func(topology, unit, hostlist string) error {
		nodes, err := hostlists.expand(ctx, hostlist)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			if !reserved[node] {
				continue
			}
			if placements[node] == nil {
				placements[node] = make(map[string]string)
			}
			placements[node][topology] = unit
		}
		return nil
	}
	show := func(topology string) (string, error) {
		out, err := run(ctx, "scontrol show topology "+framework.ShellQuote(topology))
		if err != nil {
			return "", fmt.Errorf("scontrol show topology %s: %w", topology, err)
		}
		return out, nil
	}
	for _, topoEntry := range topologyEntries {
		switch topoEntry.Kind {
		case topologyKindTree:
			out, err := show(topoEntry.Name)
			if err != nil {
				return nil, err
			}
			switches, err := parseSlurmSwitches(out)
			if err != nil {
				return nil, fmt.Errorf("parse scontrol show topology %s: %w", topoEntry.Name, err)
			}
			for _, sw := range switches {
				if !sw.IsLeaf() {
					continue
				}
				if err := place(topoEntry.Name, sw.Name, sw.Nodes); err != nil {
					return nil, err
				}
			}
		case topologyKindBlock:
			out, err := show(topoEntry.Name)
			if err != nil {
				return nil, err
			}
			blocks, err := parseSlurmBlocks(out)
			if err != nil {
				return nil, fmt.Errorf("parse scontrol show topology %s: %w", topoEntry.Name, err)
			}
			for _, block := range blocks {
				if err := place(topoEntry.Name, block.Name, block.Nodes); err != nil {
					return nil, err
				}
			}
		}
	}
	return placements, nil
}

// topologyPlacementProblems checks that each worker sits in exactly the expected topologies,
// and under no unknown switch or block in any of them.
func topologyPlacementProblems(workers []string, expected map[string][]string, placements map[string]map[string]string) []string {
	var problems []string
	for _, worker := range workers {
		units := placements[worker]
		topologies := slices.Sorted(maps.Keys(units))
		if want := expected[worker]; !slices.Equal(topologies, want) {
			problems = append(problems, fmt.Sprintf("%s is placed in topologies %v, expected %v", worker, topologies, want))
		}
		for _, topology := range topologies {
			if isUnknownTopologyUnit(units[topology]) {
				problems = append(problems, fmt.Sprintf("%s is under %s/%s", worker, topology, units[topology]))
			}
		}
	}
	return problems
}

func (s *VMStopStart) jobRunsAcrossReservedWorkers(ctx context.Context) error {
	names := s.workerNames()
	output, err := s.runtime.Jail().Run(ctx, vmStopStartSrunCommand(s.reservation, names,
		`echo "`+vmStopStartNodeKey+`=$SLURMD_NODENAME"`))
	if err != nil {
		return fmt.Errorf("run a job across reserved workers %s: %w", strings.Join(names, ","), err)
	}
	reported := map[string]bool{}
	for _, value := range parseValuesForKey(output, vmStopStartNodeKey) {
		reported[value] = true
	}
	var missing []string
	for _, name := range names {
		if !reported[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the job across reserved workers did not run on %s: %s", strings.Join(missing, ","), strings.TrimSpace(output))
	}
	s.runtime.Logf("VM stop-start: a job ran on reserved workers %s", strings.Join(names, ","))
	return nil
}

func (s *VMStopStart) jobSeesEveryGPUOfReservedGPUWorkers(ctx context.Context) error {
	byNodeSet := map[string][]framework.WorkerInfo{}
	for _, worker := range s.workers {
		if worker.HasGPU {
			byNodeSet[worker.NodeSetName] = append(byNodeSet[worker.NodeSetName], worker)
		}
	}
	if len(byNodeSet) == 0 {
		s.runtime.Logf("VM stop-start: no reserved GPU workers, nothing to check")
		return nil
	}
	var problems []error
	for _, nodeSet := range slices.Sorted(maps.Keys(byNodeSet)) {
		if err := s.runGPUJob(ctx, nodeSet, byNodeSet[nodeSet]); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// runGPUJob requests every configured GPU of each worker and checks that nvidia-smi sees all of them,
// which also exercises the NVIDIA libraries in the jail after the stop-start.
func (s *VMStopStart) runGPUJob(ctx context.Context, nodeSet string, workers []framework.WorkerInfo) error {
	gpus := workers[0].SlurmNode.GPUCount
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		if worker.SlurmNode.GPUCount != gpus || gpus < 1 {
			return fmt.Errorf("GPU workers of NodeSet %s report inconsistent gres/gpu counts: %s has %d, %s has %d",
				nodeSet, workers[0].Name, gpus, worker.Name, worker.SlurmNode.GPUCount)
		}
		names = append(names, worker.Name)
	}
	output, err := s.runtime.Jail().Run(ctx, vmStopStartSrunCommand(s.reservation, names,
		`echo "`+vmStopStartGPUsKey+`=$SLURMD_NODENAME $(nvidia-smi -L | grep -c '^GPU ')"`, "--gpus-per-node="+strconv.Itoa(gpus)))
	if err != nil {
		return fmt.Errorf("run a GPU job on reserved workers %s of NodeSet %s: %w", strings.Join(names, ","), nodeSet, err)
	}
	counts, err := parseVMStopStartGPUCounts(output)
	if err != nil {
		return err
	}
	var problems []string
	for _, name := range names {
		if counts[name] != gpus {
			problems = append(problems, fmt.Sprintf("%s sees %d GPUs", name, counts[name]))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("GPU job on NodeSet %s expected %d GPUs per worker: %s; output: %s",
			nodeSet, gpus, strings.Join(problems, ", "), strings.TrimSpace(output))
	}
	s.runtime.Logf("VM stop-start: nvidia-smi sees all %d GPUs on reserved workers %s", gpus, strings.Join(names, ","))
	return nil
}

func parseVMStopStartGPUCounts(output string) (map[string]int, error) {
	counts := map[string]int{}
	for _, value := range parseValuesForKey(output, vmStopStartGPUsKey) {
		fields := strings.Fields(value)
		if len(fields) != 2 {
			return nil, fmt.Errorf("parse GPU job report %q", value)
		}
		count, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("parse GPU count of %s in %q: %w", fields[0], value, err)
		}
		counts[fields[0]] = count
	}
	return counts, nil
}

func (s *VMStopStart) localScratchIsMounted(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	pods, err := s.kubectl.WorkerPods(ctx)
	if err != nil {
		return err
	}
	nodeSets, err := discoverLocalNVMeNodeSets(snapshot, pods)
	if err != nil {
		return err
	}
	reserved := make(map[string]bool, len(s.workers))
	for _, worker := range s.workers {
		reserved[worker.Name] = true
	}
	var problems []error
	checked := 0
	for _, nodeSet := range nodeSets {
		for _, worker := range nodeSet.Workers {
			if !reserved[worker.Name] {
				continue
			}
			checked++
			if err := s.checkLocalScratch(ctx, nodeSet, worker.Name); err != nil {
				problems = append(problems, fmt.Errorf("worker %s: %w", worker.Name, err))
			}
		}
	}
	if checked == 0 {
		s.runtime.Logf("VM stop-start: no reserved worker has node-local scratch, nothing to check")
	}
	return errors.Join(problems...)
}

func (s *VMStopStart) checkLocalScratch(ctx context.Context, nodeSet localNVMeNodeSet, worker string) error {
	output, err := s.runtime.Jail().Run(ctx, vmStopStartSrunCommand(s.reservation, []string{worker},
		framework.ShellJoin("bash", "-c", localNVMeFilesystemProbe(), "bash", nodeSet.MountPath)))
	if err != nil {
		return fmt.Errorf("probe scratch %s: %w", nodeSet.MountPath, err)
	}
	filesystem, err := parseLocalNVMeFilesystemProbe(output)
	if err != nil {
		return err
	}
	if err := validateLocalNVMeFilesystem(nodeSet, filesystem); err != nil {
		return err
	}
	s.runtime.Logf("VM stop-start: worker %s scratch %s is on device %s (%s) with capacity %d bytes",
		worker, nodeSet.MountPath, filesystem.ScratchDevice, filesystem.FilesystemType, filesystem.CapacityBytes)
	return nil
}

// vmStopStartSrunCommand runs command once on each named node inside the reservation, with extra srun flags.
// The outer timeout bounds the whole job and --immediate turns an unavailable node into a fast failure.
func vmStopStartSrunCommand(reservation string, nodes []string, command string, extraFlags ...string) string {
	args := []string{
		"timeout", strconv.Itoa(int(vmStopStartJobTimeout.Seconds())),
		"srun", fmt.Sprintf("--immediate=%d", int(vmStopStartJobImmediate.Seconds())),
		"--partition=" + framework.MainPartitionName,
		"--reservation=" + reservation,
		fmt.Sprintf("--nodes=%d", len(nodes)),
		"--ntasks-per-node=1",
		"--nodelist=" + strings.Join(nodes, ","),
	}
	args = append(args, extraFlags...)
	return framework.ShellJoin(args...) + " " + framework.ShellJoin("bash", "-c", command)
}

func (s *VMStopStart) waitForWorkers(ctx context.Context, description string, timeout time.Duration, ready func(framework.SlurmNodeInfo) bool) error {
	names := s.workerNames()
	return s.runtime.WaitFor(ctx, description, timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		nodes, err := s.slurm.NodeInfosNoRetries(waitCtx, names)
		if err != nil {
			return false, err
		}
		var pending []string
		for _, node := range nodes {
			if !ready(node) {
				pending = append(pending, fmt.Sprintf("%s state=%s reason=%q", node.Name, node.State, node.Reason))
			}
		}
		if len(pending) > 0 {
			return false, fmt.Errorf("workers not ready: %s", strings.Join(pending, "; "))
		}
		return true, nil
	})
}

func (s *VMStopStart) workerNames() []string {
	return framework.WorkerNames(s.workers)
}

func (s *VMStopStart) instanceIDs() []string {
	ids := make([]string, 0, len(s.workers))
	for _, worker := range s.workers {
		ids = append(ids, worker.SlurmNode.InstanceID)
	}
	return ids
}
