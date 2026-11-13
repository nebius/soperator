package sharedsteps

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func vmStopStartTestWorker(name, nodeSet, state string) framework.WorkerInfo {
	return framework.WorkerInfo{
		Name:        name,
		NodeSetName: nodeSet,
		SlurmNode:   framework.SlurmNodeInfo{Name: name, State: state, InstanceID: "instance-" + name},
	}
}

func vmStopStartTestSnapshot(nodeSets []framework.NodeSetInfo, workers ...framework.WorkerInfo) framework.WorkerSnapshot {
	snapshot := framework.WorkerSnapshot{NodeSets: nodeSets, WorkersByNodeSet: map[string][]framework.WorkerInfo{}}
	for _, worker := range workers {
		snapshot.Workers = append(snapshot.Workers, worker)
		snapshot.WorkersByNodeSet[worker.NodeSetName] = append(snapshot.WorkersByNodeSet[worker.NodeSetName], worker)
	}
	return snapshot
}

func TestSelectVMStopStartWorkersTakesUpToCountPerNodeSetIdleFirst(t *testing.T) {
	snapshot := vmStopStartTestSnapshot(
		[]framework.NodeSetInfo{{Name: "cpu", Replicas: 3}, {Name: "gpu", Replicas: 1, HasGPU: true}, {Name: "empty", Replicas: 0}},
		vmStopStartTestWorker("cpu-0", "cpu", "MIXED"),
		vmStopStartTestWorker("cpu-1", "cpu", "DOWN+NOT_RESPONDING"),
		vmStopStartTestWorker("cpu-2", "cpu", "IDLE"),
		vmStopStartTestWorker("gpu-0", "gpu", "IDLE"),
	)

	selected, skipped, err := selectVMStopStartWorkers(snapshot, 2)

	require.NoError(t, err)
	assert.Empty(t, skipped)
	assert.Equal(t, []string{"cpu-2", "cpu-0", "gpu-0"}, framework.WorkerNames(selected),
		"idle workers come first, an unusable worker is never picked, and a one-worker NodeSet gives one")
}

func TestSelectVMStopStartWorkersFailsOnInsufficientStaticWorkers(t *testing.T) {
	snapshot := vmStopStartTestSnapshot(
		[]framework.NodeSetInfo{{Name: "cpu", Replicas: 2}},
		vmStopStartTestWorker("cpu-0", "cpu", "IDLE"),
		vmStopStartTestWorker("cpu-1", "cpu", "IDLE+DRAIN"),
	)

	_, _, err := selectVMStopStartWorkers(snapshot, 2)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "found 1 usable workers of NodeSet cpu, need 2")
	assert.Contains(t, err.Error(), "cpu-1 state=IDLE+DRAIN")
}

func TestSelectVMStopStartWorkersSkipsEphemeralNodeSets(t *testing.T) {
	snapshot := vmStopStartTestSnapshot(
		[]framework.NodeSetInfo{{Name: "eph", Replicas: 2, Ephemeral: true}, {Name: "cpu", Replicas: 1}},
		vmStopStartTestWorker("eph-0", "eph", "IDLE"),
		vmStopStartTestWorker("eph-1", "eph", "IDLE+POWERED_DOWN"),
		vmStopStartTestWorker("cpu-0", "cpu", "IDLE"),
	)

	selected, skipped, err := selectVMStopStartWorkers(snapshot, 2)

	require.NoError(t, err)
	assert.Equal(t, []string{"cpu-0"}, framework.WorkerNames(selected))
	assert.Equal(t, []string{"skipping ephemeral NodeSet eph"}, skipped)

	selected, skipped, err = selectVMStopStartWorkers(vmStopStartTestSnapshot(snapshot.NodeSets[:1], snapshot.Workers[:2]...), 2)
	require.NoError(t, err, "a cluster of ephemeral NodeSets only is valid; the scenario skips on it")
	assert.Empty(t, selected)
	assert.Len(t, skipped, 1)
}

func TestSelectVMStopStartWorkersRequiresInstanceIDsAndWorkers(t *testing.T) {
	worker := vmStopStartTestWorker("cpu-0", "cpu", "IDLE")
	worker.SlurmNode.InstanceID = ""
	_, _, err := selectVMStopStartWorkers(vmStopStartTestSnapshot([]framework.NodeSetInfo{{Name: "cpu", Replicas: 1}}, worker), 2)
	assert.ErrorContains(t, err, "worker cpu-0 of NodeSet cpu has no InstanceId")

	_, _, err = selectVMStopStartWorkers(vmStopStartTestSnapshot([]framework.NodeSetInfo{{Name: "cpu", Replicas: 0}}), 2)
	assert.ErrorContains(t, err, "no NodeSet has workers")

	_, _, err = selectVMStopStartWorkers(vmStopStartTestSnapshot(nil), 0)
	assert.ErrorContains(t, err, "invalid worker count")
}

func TestVMStopStartSrunCommand(t *testing.T) {
	assert.Equal(t,
		"'timeout' '300' 'srun' '--immediate=120' '--partition=main' '--reservation=e2e-vm-stop-start-1' '--nodes=2' "+
			"'--ntasks-per-node=1' '--nodelist=worker-0,worker-1' '--gpus-per-node=8' 'bash' '-c' 'nvidia-smi -L'",
		vmStopStartSrunCommand("e2e-vm-stop-start-1", []string{"worker-0", "worker-1"}, "nvidia-smi -L", "--gpus-per-node=8"))
	assert.NotContains(t, vmStopStartSrunCommand("r", []string{"worker-0"}, "true"), "''",
		"no extra flags add no empty srun argument")
}

func TestParseVMStopStartJobReports(t *testing.T) {
	output := "srun: job 7 queued and waiting for resources\nvm_stop_start_node=worker-1\nvm_stop_start_node=worker-0\n"
	assert.Equal(t, []string{"worker-1", "worker-0"}, parseValuesForKey(output, vmStopStartNodeKey))

	counts, err := parseVMStopStartGPUCounts("vm_stop_start_gpus=worker-0 8\nvm_stop_start_gpus=worker-1 0\nnoise\n")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"worker-0": 8, "worker-1": 0}, counts)

	_, err = parseVMStopStartGPUCounts("vm_stop_start_gpus=worker-0\n")
	assert.ErrorContains(t, err, "parse GPU job report")
}

func TestTopologyPlacementProblems(t *testing.T) {
	expected := map[string][]string{
		"worker-0": {"tree-ib"},
		"worker-1": {"block-nvl", "tree-ib"},
		"worker-2": {"tree-ib"},
		"cpu-0":    nil,
	}
	placements := map[string]map[string]string{
		"worker-0": {"tree-ib": "sw-c"},
		"worker-1": {"tree-ib": "sw-a"},
		"worker-2": {"tree-ib": "unknown"},
	}

	assert.Equal(t, []string{
		"worker-1 is placed in topologies [tree-ib], expected [block-nvl tree-ib]",
		"worker-2 is under tree-ib/unknown",
	}, topologyPlacementProblems([]string{"worker-0", "worker-1", "worker-2", "cpu-0"}, expected, placements),
		"worker-0 moved to another switch, which is fine; cpu-0 is in no topology before or after")
}

func TestVMStopStartReservationCommand(t *testing.T) {
	assert.Equal(t,
		"'scontrol' 'create' 'reservation' 'ReservationName=e2e-vm-stop-start-1' 'Users=root' 'PartitionName=main' "+
			"'StartTime=now' 'Duration=60' 'Nodes=worker-0,cpu-0' 'Flags=IGNORE_JOBS'",
		nodeReservationCommand("e2e-vm-stop-start-1", "root", []string{"worker-0", "cpu-0"}, vmStopStartReservationDuration, "IGNORE_JOBS"))
	output := "ReservationName=e2e-vm-stop-start-1 Nodes=worker-0\nReservationName=e2e-stress-2 Nodes=worker-1\n"
	assert.Equal(t, []string{"e2e-vm-stop-start-1"}, reservationNamesWithPrefix(output, vmStopStartReservationPrefix))
}

type vmStopStartStepRuntime struct {
	computeInstanceRuntime
	jail       map[string]string
	jailErrors map[string]error
	// controller answers every controller command without a prefix match in controllerOutputs.
	controller        string
	controllerOutputs map[string]string
	commands          []string
}

func (r *vmStopStartStepRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.commands = append(r.commands, "jail: "+command)
		for prefix, err := range r.jailErrors {
			if strings.HasPrefix(command, prefix) {
				return "", err
			}
		}
		for prefix, output := range r.jail {
			if strings.HasPrefix(command, prefix) {
				return output, nil
			}
		}
		return "", nil
	})
}

func (r *vmStopStartStepRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.commands = append(r.commands, "controller: "+command)
		for prefix, output := range r.controllerOutputs {
			if strings.HasPrefix(command, prefix) {
				return output, nil
			}
		}
		return r.controller, nil
	})
}

func newVMStopStartTestSteps(runtime *vmStopStartStepRuntime) *VMStopStart {
	steps := NewVMStopStart(runtime, framework.NewSlurmClient(runtime), framework.NewKubectlClient(runtime), nil)
	steps.reservation = "e2e-vm-stop-start-1"
	steps.workers = []framework.WorkerInfo{
		vmStopStartTestWorker("worker-0", "worker", "IDLE+RESERVED"),
		vmStopStartTestWorker("worker-1", "worker", "IDLE+RESERVED"),
	}
	return steps
}

func TestControllerAnswersQueriesWithoutRetries(t *testing.T) {
	runtime := &vmStopStartStepRuntime{jail: map[string]string{"sinfo": "worker-0|2|40|drained*\nworker-1|2|40|idle*\nworker-2|2|40|idle\n"}}
	require.NoError(t, newVMStopStartTestSteps(runtime).controllerAnswers(t.Context()))
	assert.Equal(t, []string{"jail: " + partitionNodesCommand("main"), "jail: squeue -h -o '%i' >/dev/null"}, runtime.commands)

	runtime = &vmStopStartStepRuntime{jail: map[string]string{"sinfo": "worker-0|2|40|idle*\n"}}
	assert.ErrorContains(t, newVMStopStartTestSteps(runtime).controllerAnswers(t.Context()), "sinfo does not list stopped workers worker-1")

	runtime = &vmStopStartStepRuntime{jailErrors: map[string]error{"sinfo": errors.New("Unable to contact slurm controller")}}
	err := newVMStopStartTestSteps(runtime).controllerAnswers(t.Context())
	assert.ErrorContains(t, err, "Unable to contact slurm controller")
	assert.Len(t, runtime.commands, 1, "a controller that answers only after a retry is not answering")
}

func TestJobRunsAcrossReservedWorkersRequiresEveryWorker(t *testing.T) {
	runtime := &vmStopStartStepRuntime{jail: map[string]string{"'timeout'": "vm_stop_start_node=worker-1\nvm_stop_start_node=worker-0\n"}}
	require.NoError(t, newVMStopStartTestSteps(runtime).jobRunsAcrossReservedWorkers(t.Context()))
	require.Len(t, runtime.commands, 1)
	assert.Contains(t, runtime.commands[0], "'--reservation=e2e-vm-stop-start-1' '--nodes=2'")
	assert.Contains(t, runtime.commands[0], "'--nodelist=worker-0,worker-1'")

	runtime = &vmStopStartStepRuntime{jail: map[string]string{"'timeout'": "vm_stop_start_node=worker-1\n"}}
	assert.ErrorContains(t, newVMStopStartTestSteps(runtime).jobRunsAcrossReservedWorkers(t.Context()),
		"did not run on worker-0")
}

func TestGPUJobChecksEveryGPUOfEveryReservedGPUWorker(t *testing.T) {
	runtime := &vmStopStartStepRuntime{jail: map[string]string{"'timeout'": "vm_stop_start_gpus=worker-0 8\nvm_stop_start_gpus=worker-1 7\n"}}
	steps := newVMStopStartTestSteps(runtime)
	for i := range steps.workers {
		steps.workers[i].HasGPU = true
		steps.workers[i].SlurmNode.GPUCount = 8
	}
	err := steps.jobSeesEveryGPUOfReservedGPUWorkers(t.Context())
	assert.ErrorContains(t, err, "expected 8 GPUs per worker: worker-1 sees 7 GPUs")
	assert.Contains(t, runtime.commands[0], "'--gpus-per-node=8'")

	runtime = &vmStopStartStepRuntime{}
	require.NoError(t, newVMStopStartTestSteps(runtime).jobSeesEveryGPUOfReservedGPUWorkers(t.Context()))
	assert.Empty(t, runtime.commands, "CPU-only stop-starts have no GPU job to run")
}

func TestVMStopStartCleanupStartsStoppedInstancesAndReleasesReservation(t *testing.T) {
	runtime := &vmStopStartStepRuntime{
		computeInstanceRuntime: computeInstanceRuntime{states: map[string][]string{
			"instance-worker-0": {"STOPPED", "RUNNING"},
			"instance-worker-1": {"RUNNING"},
		}},
		controller: "NodeName=worker-0 State=IDLE+RESERVED\nNodeName=worker-1 State=IDLE+RESERVED\n",
	}
	steps := newVMStopStartTestSteps(runtime)
	steps.instancesStopped = true

	steps.CleanupAndReset(t.Context())

	assert.Contains(t, runtime.calls, "nebius compute instance start --id instance-worker-0 --async --format json")
	assert.NotContains(t, runtime.calls, "nebius compute instance start --id instance-worker-1 --async --format json")
	assert.Contains(t, runtime.commands, "jail: "+deleteReservationCommand("e2e-vm-stop-start-1"))
	assert.Empty(t, steps.workers)
	assert.Empty(t, steps.reservation)
	assert.False(t, steps.instancesStopped)

	runtime.commands = nil
	steps.CleanupAndReset(t.Context())
	assert.Empty(t, runtime.commands, "cleanup without tracked state does nothing")
}

const vmStopStartTestTopoconf = "- topology: tree-ib\n  cluster_default: true\n  tree:\n    switches:\n" +
	"      - switch: root\n        children: sw-c,unknown\n" +
	"      - switch: sw-c\n        nodes: worker-0\n      - switch: unknown\n        nodes: worker-1\n" +
	"- topology: block-nvl\n  block:\n    blocks:\n      - block: block-0\n        nodes: worker-[0-1]\n" +
	"- topology: flat\n  flat: true\n"

func TestReadTopologyPlacementsReadsLeavesAndBlocksOfReservedWorkers(t *testing.T) {
	runtime := &vmStopStartStepRuntime{controllerOutputs: map[string]string{
		"scontrol show topoconf": vmStopStartTestTopoconf,
		"scontrol show topology 'tree-ib'": "SwitchName=root Level=1 LinkSpeed=0 Nodes=worker-[0-1],other-0 Switches=sw-c,unknown\n" +
			"SwitchName=sw-c Level=0 LinkSpeed=0 Nodes=worker-0,other-0\n" +
			"SwitchName=unknown Level=0 LinkSpeed=0 Nodes=worker-1\n",
		"scontrol show topology 'block-nvl'": "BlockName=block-0 BlockIndex=0 Nodes=worker-[0-1] BlockSize=18\n",
		"scontrol show hostnames":            "worker-0\nworker-1\n",
	}}
	steps := newVMStopStartTestSteps(runtime)

	placements, err := steps.readTopologyPlacements(t.Context(), newCachedHostlistExpander(runtime), runtime.Controller().Run)

	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]string{
		"worker-0": {"tree-ib": "sw-c", "block-nvl": "block-0"},
		"worker-1": {"tree-ib": "unknown", "block-nvl": "block-0"},
	}, placements, "only level-0 switches place a node, workers that are not reserved are left out, and flat is skipped")
	assert.NotContains(t, runtime.commands, "controller: scontrol show topology 'flat'")
}

func TestReservedWorkersRejoinTopologiesAcceptsAMoveButNotUnknown(t *testing.T) {
	newSteps := func(tree string) *VMStopStart {
		runtime := &vmStopStartStepRuntime{controllerOutputs: map[string]string{
			"scontrol show topoconf":           "- topology: tree-ib\n  tree:\n    switches:\n      - switch: sw-a\n        nodes: worker-[0-1]\n",
			"scontrol show topology 'tree-ib'": tree,
		}}
		steps := newVMStopStartTestSteps(runtime)
		steps.topologies = map[string][]string{"worker-0": {"tree-ib"}, "worker-1": {"tree-ib"}}
		return steps
	}

	steps := newSteps("SwitchName=root Level=1 Nodes=worker-0,worker-1\nSwitchName=sw-c Level=0 Nodes=worker-0\nSwitchName=sw-d Level=0 Nodes=worker-1\n")
	require.NoError(t, steps.reservedWorkersRejoinTopologies(t.Context()),
		"both workers started on hosts under other switches, and slurmctld places them there")

	steps = newSteps("SwitchName=root Level=1 Nodes=worker-0,worker-1\nSwitchName=sw-c Level=0 Nodes=worker-0\nSwitchName=unknown Level=0 Nodes=worker-1\n")
	assert.ErrorContains(t, steps.reservedWorkersRejoinTopologies(t.Context()), "worker-1 is under tree-ib/unknown")

	steps = newSteps("SwitchName=root Level=1 Nodes=worker-0\nSwitchName=sw-c Level=0 Nodes=worker-0\n")
	assert.ErrorContains(t, steps.reservedWorkersRejoinTopologies(t.Context()), "worker-1 is placed in topologies [], expected [tree-ib]")
}

func TestDrainOneWorkerUsesAnUnownedReasonAndVerifiesTheDrain(t *testing.T) {
	drained := "State=IDLE+DRAIN+RESERVED Reason=" + vmStopStartDrainReason + " [root@2026-11-11T10:00:00]"
	runtime := &vmStopStartStepRuntime{controllerOutputs: map[string]string{
		"scontrol -o show node ": "NodeName=worker-0 " + drained + "\nNodeName=worker-1 " + drained + "\n",
	}}
	steps := newVMStopStartTestSteps(runtime)

	require.NoError(t, steps.drainOneWorker(t.Context()))

	assert.Contains(t, []string{"worker-0", "worker-1"}, steps.drainedWorker)
	assert.Contains(t, runtime.commands,
		"jail: 'scontrol' 'update' 'nodename="+steps.drainedWorker+"' 'state=DRAIN' 'reason=e2e vm stop-start manual drain'")
	assert.NotContains(t, vmStopStartDrainReason, "[", "a bracketed prefix could match a reason a Soperator controller or check owns and clears")

	runtime = &vmStopStartStepRuntime{controllerOutputs: map[string]string{
		"scontrol -o show node ": "NodeName=worker-0 State=IDLE+RESERVED\nNodeName=worker-1 State=IDLE+RESERVED\n",
	}}
	steps = newVMStopStartTestSteps(runtime)
	err := steps.drainOneWorker(t.Context())
	require.NotEmpty(t, steps.drainedWorker, "ownership is recorded before the request so cleanup still resumes it")
	assert.ErrorContains(t, err, "worker "+steps.drainedWorker+" is not drained after the drain request")
}

func TestReturnToServiceAcceptsTheDrainedWorkerOnlyWhenItRespondsAgain(t *testing.T) {
	runtime := &vmStopStartStepRuntime{controllerOutputs: map[string]string{
		"scontrol -o show node ": "NodeName=worker-0 State=IDLE+DRAIN+RESERVED Reason=manual\nNodeName=worker-1 State=IDLE+RESERVED\n",
	}}
	steps := newVMStopStartTestSteps(runtime)
	steps.drainedWorker = "worker-0"
	require.NoError(t, steps.reservedWorkersReturnToService(t.Context()))

	steps.drainedWorker = ""
	assert.ErrorContains(t, steps.reservedWorkersReturnToService(t.Context()), `worker-0 state=IDLE+DRAIN+RESERVED reason="manual"`,
		"a worker nobody drained must come back without a drain")
}

func TestDrainedWorkerKeepsDrainReportsAClearedDrain(t *testing.T) {
	reason := vmStopStartDrainReason
	runtime := &vmStopStartStepRuntime{controllerOutputs: map[string]string{"scontrol -o show node ": "NodeName=worker-0 State=IDLE+RESERVED\n"}}
	steps := newVMStopStartTestSteps(runtime)
	steps.drainedWorker = "worker-0"

	assert.ErrorContains(t, steps.drainedWorkerKeepsDrain(t.Context()), "the stop-start cleared the manual drain of worker worker-0")

	runtime.controllerOutputs["scontrol -o show node "] = "NodeName=worker-0 State=IDLE+DRAIN+RESERVED Reason=" + reason + " [root@2026-11-11T10:00:00]\n"
	require.NoError(t, steps.drainedWorkerKeepsDrain(t.Context()))
}

func TestVMStopStartCleanupResumesOnlyItsOwnDrainBeforeWaiting(t *testing.T) {
	reason := vmStopStartDrainReason
	runtime := &vmStopStartStepRuntime{
		computeInstanceRuntime: computeInstanceRuntime{states: map[string][]string{
			"instance-worker-0": {"RUNNING"},
			"instance-worker-1": {"RUNNING"},
		}},
		controllerOutputs: map[string]string{
			"scontrol show node 'worker-0'": "NodeName=worker-0 State=IDLE+DRAIN+RESERVED Reason=" + reason + " [root@2026-11-11T10:00:00]",
			"scontrol -o show node ":        "NodeName=worker-0 State=IDLE+RESERVED\nNodeName=worker-1 State=IDLE+RESERVED\n",
		},
	}
	steps := newVMStopStartTestSteps(runtime)
	steps.instancesStopped = true
	steps.drainedWorker = "worker-0"

	steps.CleanupAndReset(t.Context())

	resume := slices.Index(runtime.commands, "controller: scontrol update nodename='worker-0' state=resume")
	wait := slices.Index(runtime.commands, "controller: scontrol -o show node 'worker-0,worker-1'")
	require.NotEqual(t, -1, resume)
	require.NotEqual(t, -1, wait)
	assert.Less(t, resume, wait, "a drained worker never becomes usable, so the drain is resumed before the wait")

	runtime = &vmStopStartStepRuntime{controllerOutputs: map[string]string{
		"scontrol show node 'worker-0'": "NodeName=worker-0 State=IDLE+DRAIN Reason=somebody else's drain",
	}}
	steps = newVMStopStartTestSteps(runtime)
	steps.drainedWorker = "worker-0"
	steps.CleanupAndReset(t.Context())
	assert.NotContains(t, runtime.commands, "controller: scontrol update nodename='worker-0' state=resume",
		"a drain with another reason belongs to someone else")
}

func TestStopVMsFailsWithTheComputeErrorAndKeepsOwnership(t *testing.T) {
	runtime := &vmStopStartStepRuntime{computeInstanceRuntime: computeInstanceRuntime{operations: map[string][]string{
		"op-stop-instance-worker-0": {`{"id":"op-stop-instance-worker-0","finished_at":"2026-11-12T10:00:00Z","status":{}}`},
		"op-stop-instance-worker-1": {`{"id":"op-stop-instance-worker-1","description":"Stop Instance","resource_id":"instance-worker-1",` +
			`"finished_at":"2026-11-12T10:00:00Z","status":{"code":9,"message":"instance is being maintained"}}`},
	}}}
	steps := newVMStopStartTestSteps(runtime)

	err := steps.stopVMs(t.Context())

	assert.ErrorContains(t, err, "Compute operation op-stop-instance-worker-1 (Stop Instance of instance-worker-1) failed with code 9: instance is being maintained")
	assert.True(t, steps.instancesStopped, "cleanup must start whatever the scenario tried to stop")
	assert.NotContains(t, runtime.calls, "nebius compute instance get --id instance-worker-0 --format json",
		"a failed operation ends the step before the instance state wait")
}

func TestVMStopStartCleanupWaitsForInFlightStopBeforeReadingStates(t *testing.T) {
	runtime := &vmStopStartStepRuntime{
		computeInstanceRuntime: computeInstanceRuntime{
			operations: map[string][]string{"op-stop-instance-worker-0": {
				`{"id":"op-stop-instance-worker-0"}`,
				`{"id":"op-stop-instance-worker-0","finished_at":"2026-11-12T10:00:00Z","status":{}}`,
			}},
			states: map[string][]string{"instance-worker-0": {"STOPPED", "RUNNING"}, "instance-worker-1": {"RUNNING"}},
		},
		controller: "NodeName=worker-0 State=IDLE+RESERVED\nNodeName=worker-1 State=IDLE+RESERVED\n",
	}
	steps := newVMStopStartTestSteps(runtime)
	steps.instancesStopped = true
	steps.operations = []string{"op-stop-instance-worker-0"}

	steps.CleanupAndReset(t.Context())

	lastOperation := slices.Index(runtime.calls, "nebius compute instance operation get --id op-stop-instance-worker-0 --format json")
	firstState := slices.Index(runtime.calls, "nebius compute instance get --id instance-worker-0 --format json")
	require.NotEqual(t, -1, firstState)
	assert.Less(t, lastOperation, firstState,
		"an instance still RUNNING right after its stop request would otherwise be left alone and stop afterwards")
	assert.Contains(t, runtime.calls, "nebius compute instance start --id instance-worker-0 --async --format json")
}
