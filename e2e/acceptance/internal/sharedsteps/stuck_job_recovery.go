package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	stuckJobDrainReason = "Kill task failed"
	// Recovery includes the soperatorchecks reconciliation (three minutes by default),
	// VM reboot, and worker startup.
	stuckJobRecoveryTimeout = 20 * time.Minute
)

type StuckJobRecovery struct {
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	worker        framework.WorkerInfo
	host          corev1.Node
	faultInjected bool
	monitoring    *stuckJobMonitoring
}

type stuckJobReservationKey struct{}

func NewStuckJobRecovery(runtime framework.Runtime, slurm *framework.SlurmClient, kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *StuckJobRecovery {
	return &StuckJobRecovery{runtime: runtime, slurm: slurm, kubectl: kubectl, selector: selector}
}

func (s *StuckJobRecovery) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^an idle GPU worker is reserved for stuck-job recovery$`, s.reserveWorker)
	sc.Step(`^a stuck-job drain is simulated on that worker$`, s.simulateDrain)
	sc.Step(`^the worker goes down and reboots$`, s.waitForReboot)
	sc.Step(`^the worker returns to service automatically$`, s.waitForRecovery)
	sc.Step(`^a job can use all GPUs on that worker$`, s.runGPUJob)
	sc.Step(`^monitoring is recording that worker$`, s.monitoringRecordsWorker)
	sc.Step(`^monitoring records the stuck-job drain, down state and recovery$`, s.monitoringRecordsRecovery)
	// SoperatorSuite does not yet enable reservation scopes for the whole suite.
	// This runs after the family's cleanup, while the lease still protects recovery.
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if _, owned := ctx.Value(stuckJobReservationKey{}).(bool); !owned {
			return ctx, nil
		}
		return ctx, framework.FinishWorkerReservation(ctx, s.runtime)
	})
}

func (s *StuckJobRecovery) CleanupAndReset(ctx context.Context) {
	if s.faultInjected {
		if err := s.waitForRecovery(ctx); err != nil {
			s.runtime.Logf("Cleanup: recover worker %s after stuck-job drain: %v", s.worker.Name, err)
			framework.RecordWorkerCleanupError(ctx, err)
		}
	}
	s.worker = framework.WorkerInfo{}
	s.host = corev1.Node{}
	s.faultInjected = false
	s.monitoring = nil
}

func (s *StuckJobRecovery) reserveWorker(ctx context.Context) (context.Context, error) {
	ctx = framework.WithWorkerReservation(ctx, framework.NewReservationManager(), "stuck-job recovery")
	ctx = context.WithValue(ctx, stuckJobReservationKey{}, true)
	return ctx, s.selectWorker(ctx)
}

func (s *StuckJobRecovery) selectWorker(ctx context.Context) error {
	workers, err := s.selector.Acquire(ctx, framework.WorkerRequirements{Kind: "GPU", Count: 1, Idle: true})
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	pod, err := s.kubectl.WorkerPodForSlurmNode(ctx, s.worker.Name)
	if err != nil {
		return err
	}
	host, present, err := s.kubectl.NodeIfPresent(ctx, pod.KubernetesNodeName)
	if err != nil {
		return err
	}
	if !present || host.Status.NodeInfo.BootID == "" || s.worker.SlurmNode.InstanceID == "" || s.worker.SlurmNode.GPUCount < 1 {
		return fmt.Errorf("record worker %s reboot baseline: host=%s bootID=%s instance=%s GPUs=%d",
			s.worker.Name, host.Name, host.Status.NodeInfo.BootID, s.worker.SlurmNode.InstanceID, s.worker.SlurmNode.GPUCount)
	}
	s.host = host
	s.runtime.Logf("Stuck-job recovery baseline: worker=%s host=%s UID=%s bootID=%s instance=%s GPUs=%d",
		s.worker.Name, host.Name, host.UID, host.Status.NodeInfo.BootID, s.worker.SlurmNode.InstanceID, s.worker.SlurmNode.GPUCount)
	return nil
}

func (s *StuckJobRecovery) simulateDrain(ctx context.Context) error {
	if err := framework.RequireReservedHost(ctx, s.kubectl, s.host.Name); err != nil {
		return err
	}
	// A lost command response may still have applied the drain.
	if s.monitoring != nil {
		s.monitoring.since = time.Now()
	}
	s.faultInjected = true
	_, err := s.runtime.Controller().Run(ctx, framework.ShellJoin("scontrol", "update", "nodename="+s.worker.Name, "state=DRAIN", "reason="+stuckJobDrainReason))
	return err
}

func (s *StuckJobRecovery) waitForReboot(ctx context.Context) error {
	var downSeen bool
	return s.runtime.WaitFor(ctx, "worker "+s.worker.Name+" DOWN and rebooted", stuckJobRecoveryTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			node, err := s.slurm.NodeInfoOnce(waitCtx, s.worker.Name)
			if err != nil {
				return false, err
			}
			if node.HasStateFlag("DOWN") && !downSeen {
				downSeen = true
				s.runtime.Logf("Stuck-job recovery: worker=%s state=%s reason=%s", node.Name, node.State, node.Reason)
			}
			host, present, err := s.kubectl.NodeIfPresent(waitCtx, s.host.Name)
			if err != nil {
				return false, err
			}
			if err := s.checkHostIdentity(node, host, present); err != nil {
				return false, err
			}
			if downSeen && present && host.Status.NodeInfo.BootID != "" && host.Status.NodeInfo.BootID != s.host.Status.NodeInfo.BootID {
				s.runtime.Logf("Stuck-job recovery: host=%s bootID changed from %s to %s", host.Name, s.host.Status.NodeInfo.BootID, host.Status.NodeInfo.BootID)
				return true, nil
			}
			return false, fmt.Errorf("worker %s: state=%s reason=%q DOWN observed=%t bootID=%s",
				node.Name, node.State, node.Reason, downSeen, host.Status.NodeInfo.BootID)
		})
}

func (s *StuckJobRecovery) checkHostIdentity(node framework.SlurmNodeInfo, host corev1.Node, present bool) error {
	if node.InstanceID != s.worker.SlurmNode.InstanceID || present && host.UID != s.host.UID {
		return &framework.PermanentError{Err: fmt.Errorf("worker %s was replaced instead of rebooted: instance=%s expected=%s host UID=%s expected=%s",
			node.Name, node.InstanceID, s.worker.SlurmNode.InstanceID, host.UID, s.host.UID)}
	}
	return nil
}

func (s *StuckJobRecovery) waitForRecovery(ctx context.Context) error {
	return s.runtime.WaitFor(ctx, "worker "+s.worker.Name+" recovered after stuck-job drain", stuckJobRecoveryTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			node, err := s.slurm.NodeInfoOnce(waitCtx, s.worker.Name)
			if err != nil {
				return false, err
			}
			host, present, err := s.kubectl.NodeIfPresent(waitCtx, s.host.Name)
			if err != nil {
				return false, err
			}
			if err := s.checkHostIdentity(node, host, present); err != nil {
				return false, err
			}
			if !present || !k8sNodeReady(host) || host.Spec.Unschedulable || host.Status.NodeInfo.BootID == "" ||
				host.Status.NodeInfo.BootID == s.host.Status.NodeInfo.BootID || !node.IsIdle() {
				return false, fmt.Errorf("worker %s: state=%s reason=%q host Ready=%t unschedulable=%t bootID=%s",
					node.Name, node.State, node.Reason, k8sNodeReady(host), host.Spec.Unschedulable, host.Status.NodeInfo.BootID)
			}
			pod, err := s.kubectl.WorkerPodForSlurmNode(waitCtx, s.worker.Name)
			if err != nil {
				return false, err
			}
			if !pod.Ready || pod.KubernetesNodeName != s.host.Name {
				return false, fmt.Errorf("worker %s pod=%s Ready=%t host=%s expected=%s", node.Name, pod.PodName, pod.Ready, pod.KubernetesNodeName, s.host.Name)
			}
			s.runtime.Logf("Stuck-job recovery: worker=%s is IDLE without drain on Ready host %s", node.Name, host.Name)
			return true, nil
		})
}

func (s *StuckJobRecovery) runGPUJob(ctx context.Context) error {
	jobCtx, cancel := context.WithTimeout(ctx, framework.TargetedWorkerSmokeTimeout)
	defer cancel()
	gpus := s.worker.SlurmNode.GPUCount
	output, err := s.slurm.RunSlurm(jobCtx, "srun", framework.SlurmCommandOptions{
		Nodelist: []string{s.worker.Name}, TimeLimit: 3 * time.Minute,
		Args: []string{fmt.Sprintf("--gpus-per-node=%d", gpus), "--ntasks=1"},
	}, "nvidia-smi", "-L")
	if err != nil {
		return err
	}
	if count := strings.Count("\n"+output, "\nGPU "); count != gpus {
		return fmt.Errorf("worker %s GPU job sees %d GPUs, expected %d: %s", s.worker.Name, count, gpus, output)
	}
	s.runtime.Logf("Stuck-job recovery: job sees all %d GPUs on worker %s", gpus, s.worker.Name)
	return nil
}
