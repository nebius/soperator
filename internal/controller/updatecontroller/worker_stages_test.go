package updatecontroller

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

func requireWorkerCounts(t *testing.T, f *metricFixture, expected map[string]int) {
	t.Helper()
	for _, stage := range workerStageNames {
		require.Equal(t, float64(expected[stage]), metricValue(f.r.metrics.workers, stage), stage)
	}
}

func TestWorkerStagesPartitionMixedRolloutAndClearDisappearedWorkers(t *testing.T) {
	f := newMetricFixture(t)
	sts := f.sts(t)
	sts.Spec.Replicas, sts.Status.ReadyReplicas = ptr.To(int32(7)), 5
	setMaxUnavailable(sts, intstr.FromInt32(5))
	require.NoError(t, f.r.Update(t.Context(), sts))
	base := &corev1.Pod{}
	require.NoError(t, f.r.Get(t.Context(), f.podKey, base))
	f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE)
	for i := 1; i < 6; i++ {
		pod := base.DeepCopy()
		pod.Name, pod.UID, pod.ResourceVersion = fmt.Sprintf("worker-%d", i), types.UID(fmt.Sprintf("uid-%d", i)), ""
		node := slurmapi.Node{Name: pod.Name, States: nodeStates(api.V0044NodeStateREBOOTREQUESTED)}
		switch i {
		case 1:
			node.AllocCPUs = ptr.To(int32(4))
		case 3:
			node.States = nodeStates(api.V0044NodeStateREBOOTISSUED)
		case 4, 5:
			pod.Labels["controller-revision-hash"] = "current-revision"
			node.States = nodeStates(api.V0044NodeStateIDLE)
			if i == 4 {
				pod.Status.Conditions[0].Status = corev1.ConditionFalse
			}
		}
		require.NoError(t, f.r.Create(t.Context(), pod))
		f.nodes = append(f.nodes, node)
	}
	f.run(t)
	require.Equal(t, 1.0, metricValue(f.r.metrics.waiting, waitBudget))
	requireWorkerCounts(t, f, map[string]int{"ready": 1, "waiting_for_slot": 1, "waiting_for_jobs": 1, "waiting_for_slurm": 1, "stopping_worker": 1, "starting_pod": 1, "waiting_for_pod": 1})
	for _, name := range []string{"worker-0", "worker-3"} {
		pod := &corev1.Pod{}
		require.NoError(t, f.r.Get(t.Context(), client.ObjectKey{Namespace: sts.Namespace, Name: name}, pod))
		require.NoError(t, f.r.Delete(t.Context(), pod))
	}
	f.run(t)
	requireWorkerCounts(t, f, map[string]int{"ready": 1, "waiting_for_jobs": 1, "waiting_for_slurm": 1, "starting_pod": 1, "waiting_for_pod": 3})
	f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
		return errors.New("API unavailable")
	}})
	f.run(t)
	requireWorkerCounts(t, f, map[string]int{"unknown": 7})
}

func TestWorkerStagesRequireAllocationEvidenceForWaitingJobs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cpu        *int32
		memory     *int64
		completing bool
		stage      string
	}{
		{name: "unknown allocations", stage: "waiting_for_slurm"},
		{name: "zero allocations", cpu: ptr.To(int32(0)), memory: ptr.To(int64(0)), stage: "waiting_for_slurm"},
		{name: "CPU allocation", cpu: ptr.To(int32(2)), stage: "waiting_for_jobs"},
		{name: "memory allocation", memory: ptr.To(int64(1024)), stage: "waiting_for_jobs"},
		{name: "completing", completing: true, stage: "waiting_for_jobs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMetricFixture(t)
			// Exercise a newly acknowledged request, not just an observed reboot flag.
			f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE)
			if tc.completing {
				f.nodes[0].States = nodeStates(api.V0044NodeStateCOMPLETING)
			}
			f.nodes[0].AllocCPUs, f.nodes[0].AllocMemoryMB = tc.cpu, tc.memory
			f.run(t)
			requireWorkerCounts(t, f, map[string]int{tc.stage: 1})
		})
	}
}

func TestWorkerStagesDoNotInventReadyOrStarting(t *testing.T) {
	for _, mode := range []string{"empty target", "stale generation", "Slurm read failure", "cached cleanup read failure", "old unready worker"} {
		t.Run(mode, func(t *testing.T) {
			f := newMetricFixture(t)
			f.pod(t, func(p *corev1.Pod) { p.Labels["controller-revision-hash"] = "current-revision" })
			f.nodes[0] = slurmapi.Node{Name: f.podKey.Name, States: nodeStates(api.V0044NodeStateIDLE)}
			expected := "unknown"
			switch mode {
			case "empty target", "stale generation":
				sts := f.sts(t)
				if mode == "empty target" {
					sts.Status.UpdateRevision = ""
				} else {
					sts.Generation = sts.Status.ObservedGeneration + 1
				}
				require.NoError(t, f.r.Update(t.Context(), sts))
			case "Slurm read failure":
				f.listErr = errors.New("Slurm unavailable")
			case "cached cleanup read failure":
				f.run(t)
				f.run(t)
				requireWorkerCounts(t, f, map[string]int{"ready": 1})
				f.clock.Step(time.Hour)
				f.listErr = errors.New("Slurm unavailable")
			case "old unready worker":
				f.pod(t, func(p *corev1.Pod) {
					p.Labels["controller-revision-hash"] = "old-revision"
					p.Status.Conditions[0].Status = corev1.ConditionFalse
				})
				expected = "waiting_for_slot"
			}
			f.run(t)
			requireWorkerCounts(t, f, map[string]int{expected: 1})
		})
	}
}

func TestWorkerStagesKeepKnownPartialDeletionAndReportUnvisitedWorkers(t *testing.T) {
	f := newMetricFixture(t)
	f.pod(t, func(p *corev1.Pod) {
		p.Labels[consts.LabelSoperatorWorkerOperationID] = "current-revision"
		p.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
	})
	pod := &corev1.Pod{}
	require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
	for i := 1; i < 3; i++ {
		other := pod.DeepCopy()
		other.Name, other.UID, other.ResourceVersion = fmt.Sprintf("worker-%d", i), types.UID(fmt.Sprintf("uid-%d", i)), ""
		require.NoError(t, f.r.Create(t.Context(), other))
	}
	f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
		if obj.GetName() == "worker-1" {
			return errors.New("delete blocked")
		}
		return c.Delete(ctx, obj, opts...)
	}})
	f.run(t)
	// Owned pods can exceed desired during scale-down. A successful delete remains
	// present in this snapshot, and later unvisited workers are not claimed ready.
	requireWorkerCounts(t, f, map[string]int{"deleting_pod": 1, "blocked": 1, "unknown": 1})
}
