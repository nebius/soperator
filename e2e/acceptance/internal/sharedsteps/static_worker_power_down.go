package sharedsteps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	staticPowerDownStartTimeout   = 2 * time.Minute
	staticPowerDownPollInterval   = 2 * time.Second
	staticPowerDownRecoverTimeout = 10 * time.Minute
	staticPowerDownCompleteMargin = time.Minute
	// staticPowerDownControllerSlack covers the operator's 10s deadline margin and the delay before
	// power-manager records the request.
	staticPowerDownControllerSlack = 30 * time.Second
	// staticPowerDownRecreatedGrace is how long cleanup lets a pod recreated by Soperator start.
	staticPowerDownRecreatedGrace = 5 * time.Minute
	nodeSetPowerStateCRDName      = "nodesetpowerstates.slurm.nebius.ai"
)

// StaticWorkerPowerDown covers a manual Slurm power down of a static worker: Soperator restarts
// the worker pod once the power down completes, and the fresh slurmd returns the node to service.
//
// Deprecated: covers the static worker feature, which will be removed, see
// features/static_worker_power_down.feature.
type StaticWorkerPowerDown struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	suspendTimeout     time.Duration
	target             staticPowerDownTarget
	powerDownRequested bool
	powerDownAt        time.Time
}

type staticPowerDownTarget struct {
	worker  framework.WorkerInfo
	podName string
	podUID  string
}

func NewStaticWorkerPowerDown(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *StaticWorkerPowerDown {
	return &StaticWorkerPowerDown{
		info:     info,
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *StaticWorkerPowerDown) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^an idle static worker is selected for a power down$`, s.selectIdleStaticWorker)
	sc.Step(`^the selected static worker is powered down through Slurm$`, s.powerDownSelectedWorker)
	sc.Step(`^Slurm powers the selected static worker down$`, s.waitForPowerDown)
	sc.Step(`^the selected static worker returns to service in a recreated pod$`, s.waitForReturnToService)
	sc.Step(`^the selected static worker runs a job$`, s.runJobOnSelectedWorker)
}

func (s *StaticWorkerPowerDown) CleanupAndReset(ctx context.Context) {
	if s.powerDownRequested {
		cleanupCtx, cancel := context.WithTimeout(ctx,
			s.suspendTimeout+staticPowerDownControllerSlack+staticPowerDownCompleteMargin+
				staticPowerDownRecreatedGrace+staticPowerDownRecoverTimeout)
		defer cancel()
		if err := s.restoreWorker(cleanupCtx); err != nil {
			s.runtime.Logf("cleanup: restore powered down static worker %s (pod %s/%s): %v",
				s.target.worker.Name, framework.SoperatorNamespace, s.target.podName, err)
		}
	}
	s.suspendTimeout = 0
	s.target = staticPowerDownTarget{}
	s.powerDownRequested = false
	s.powerDownAt = time.Time{}
}

func (s *StaticWorkerPowerDown) selectIdleStaticWorker(ctx context.Context) error {
	// TODO: Replace this check with a version constraint once the first release that recreates
	// powered down static workers is known: both main and soperator-release-5.0 are 5.0.0 now.
	supported, err := s.powerStateHasRecreateNodes(ctx)
	if err != nil {
		return err
	}
	if !supported {
		s.runtime.Logf("acceptance: %s has no spec.recreateNodes, so powered down static workers are not recreated, skipping scenario",
			nodeSetPowerStateCRDName)
		return godog.ErrSkip
	}

	configuration, err := s.slurm.Configuration(ctx)
	if err != nil {
		return err
	}
	suspendTimeout, err := parseSlurmSeconds(configuration["SuspendTimeout"])
	if err != nil {
		return fmt.Errorf("parse SuspendTimeout %q: %w", configuration["SuspendTimeout"], err)
	}

	nodeSets, err := listClusterNodeSets(ctx, s.kubectl, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	worker, err := pickIdleStaticWorker(nodeSets, snapshot.Workers)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	if worker.SlurmNode.SlurmdStartTime.IsZero() {
		return fmt.Errorf("read SlurmdStartTime of Slurm node %s", worker.Name)
	}

	podInfo, err := s.kubectl.WorkerPodForSlurmNode(ctx, worker.Name)
	if err != nil {
		return err
	}
	pod, found, err := s.kubectl.PodIfPresent(ctx, framework.SoperatorNamespace, podInfo.PodName)
	if err != nil {
		return err
	}
	if !found || !kubeobjects.PodReady(pod) {
		return fmt.Errorf("worker pod %s of Slurm node %s is not ready", podInfo.PodName, worker.Name)
	}

	s.suspendTimeout = suspendTimeout
	s.target = staticPowerDownTarget{
		worker:  worker,
		podName: pod.Name,
		podUID:  string(pod.UID),
	}
	s.runtime.Logf("Static worker power down: selected %s (NodeSet %s, pod %s, UID %s, SuspendTimeout %s)",
		worker.Name, worker.NodeSetName, pod.Name, pod.UID, suspendTimeout)
	return nil
}

func (s *StaticWorkerPowerDown) powerDownSelectedWorker(ctx context.Context) error {
	if s.target.worker.Name == "" {
		return fmt.Errorf("static worker is not selected")
	}
	// Keep ownership even if the command applies but its response is lost.
	s.powerDownRequested = true
	s.powerDownAt = time.Now()
	// Slurm ignores a repeated power down while the node is powering down, so a retry is safe.
	if _, err := s.runtime.Controller().RunWithDefaultRetry(ctx,
		staticPowerDownCommand(s.target.worker.Name)); err != nil {
		return fmt.Errorf("power down Slurm node %s: %w", s.target.worker.Name, err)
	}
	return nil
}

func (s *StaticWorkerPowerDown) waitForPowerDown(ctx context.Context) error {
	name := s.target.worker.Name
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Slurm node %s to power down", name),
		staticPowerDownStartTimeout, staticPowerDownPollInterval,
		func(waitCtx context.Context) (bool, error) {
			node, err := s.slurm.NodeInfoOnce(waitCtx, name)
			if err != nil {
				return false, err
			}
			if !isPoweringOrPoweredDown(node) {
				return false, fmt.Errorf("Slurm node %s has state=%s reason=%s", name, node.State, node.Reason)
			}
			return true, nil
		},
	)
}

func (s *StaticWorkerPowerDown) waitForReturnToService(ctx context.Context) error {
	return s.runtime.WaitFor(ctx,
		fmt.Sprintf("Slurm node %s to return to service in a recreated pod", s.target.worker.Name),
		s.suspendTimeout+staticPowerDownRecoverTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			pod, found, err := s.kubectl.PodIfPresent(waitCtx, framework.SoperatorNamespace, s.target.podName)
			if err != nil {
				return false, err
			}
			node, err := s.slurm.NodeInfoOnce(waitCtx, s.target.worker.Name)
			if err != nil {
				return false, err
			}
			if err := checkStaticWorkerReturned(s.target, pod, found, node); err != nil {
				return false, err
			}
			return true, nil
		},
	)
}

func (s *StaticWorkerPowerDown) runJobOnSelectedWorker(ctx context.Context) error {
	return framework.RunTargetedWorkerSmoke(ctx, s.runtime, s.target.worker)
}

// restoreWorker returns a worker left powered down by a failed run to service. Slurm ignores a
// slurmd that starts while the node is powering down, and Soperator recreates the pod itself once
// SuspendTimeout has passed, so both are waited out before the pod is deleted here.
func (s *StaticWorkerPowerDown) restoreWorker(ctx context.Context) error {
	name := s.target.worker.Name
	recreateDeadline := s.powerDownAt.Add(s.suspendTimeout + staticPowerDownControllerSlack)
	var node framework.SlurmNodeInfo
	if err := s.runtime.WaitFor(ctx, fmt.Sprintf("Slurm node %s power down and pod recreation deadline", name),
		s.suspendTimeout+staticPowerDownControllerSlack+staticPowerDownCompleteMargin, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			var err error
			if node, err = s.slurm.NodeInfoOnce(waitCtx, name); err != nil {
				return false, err
			}
			return !isPowerDownInProgress(node) && !time.Now().Before(recreateDeadline), nil
		},
	); err != nil {
		return err
	}
	if node.IsReadyForImmediateWork() {
		return nil
	}

	pod, found, err := s.kubectl.PodIfPresent(ctx, framework.SoperatorNamespace, s.target.podName)
	if err != nil {
		return err
	}
	if !found || string(pod.UID) != s.target.podUID {
		// Soperator has recreated the pod. One recreated too early leaves the node powered down, so it
		// is deleted again unless the node comes back.
		s.runtime.Logf("cleanup: waiting for the recreated worker pod %s/%s of Slurm node %s",
			framework.SoperatorNamespace, s.target.podName, name)
		if err := s.waitForWorkerInService(ctx, staticPowerDownRecreatedGrace); err == nil {
			return nil
		}
	}

	s.runtime.Logf("cleanup: deleting worker pod %s/%s of Slurm node %s with state=%s reason=%s",
		framework.SoperatorNamespace, s.target.podName, name, node.State, node.Reason)
	if _, err := s.runtime.Kubectl().Run(ctx,
		"delete", "pod", s.target.podName, "-n", framework.SoperatorNamespace, "--ignore-not-found", "--wait=false",
	); err != nil {
		return fmt.Errorf("delete worker pod %s: %w", s.target.podName, err)
	}
	return s.waitForWorkerInService(ctx, staticPowerDownRecoverTimeout)
}

func (s *StaticWorkerPowerDown) waitForWorkerInService(ctx context.Context, timeout time.Duration) error {
	name := s.target.worker.Name
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Slurm node %s to return to service after cleanup", name),
		timeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			node, err := s.slurm.NodeInfoOnce(waitCtx, name)
			if err != nil {
				return false, err
			}
			if !node.IsReadyForImmediateWork() {
				return false, fmt.Errorf("Slurm node %s has state=%s reason=%s", name, node.State, node.Reason)
			}
			return true, nil
		},
	)
}

func (s *StaticWorkerPowerDown) powerStateHasRecreateNodes(ctx context.Context) (bool, error) {
	var crd customResourceDefinition
	if err := s.kubectl.GetJSON(ctx, &crd, "get", "crd", nodeSetPowerStateCRDName, "-o", "json"); err != nil {
		return false, fmt.Errorf("get CRD %s: %w", nodeSetPowerStateCRDName, err)
	}
	return crdSpecHasField(crd, "recreateNodes"), nil
}

type customResourceDefinition struct {
	Spec struct {
		Versions []struct {
			Served bool `json:"served"`
			Schema struct {
				OpenAPIV3Schema crdSchemaProps `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

type crdSchemaProps struct {
	Properties map[string]crdSchemaProps `json:"properties"`
}

// crdSpecHasField reports whether a served version of the CRD declares the spec field.
func crdSpecHasField(crd customResourceDefinition, field string) bool {
	for _, version := range crd.Spec.Versions {
		if !version.Served {
			continue
		}
		if _, ok := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties[field]; ok {
			return true
		}
	}
	return false
}

// pickIdleStaticWorker prefers CPU workers, whose pods restart faster. A cluster without static
// workers lacks an optional prerequisite; static workers that are all busy or unhealthy are a failure.
func pickIdleStaticWorker(nodeSets []kubeobjects.NodeSet, workers []framework.WorkerInfo) (framework.WorkerInfo, error) {
	staticNodeSets := make(map[string]bool, len(nodeSets))
	for _, nodeSet := range nodeSets {
		if nodeSet.Spec.Replicas > 0 && !isEphemeralNodeSet(nodeSet) {
			staticNodeSets[nodeSet.Metadata.Name] = true
		}
	}

	var (
		candidates []framework.WorkerInfo
		problems   []string
	)
	for _, worker := range workers {
		if !staticNodeSets[worker.NodeSetName] {
			continue
		}
		if !isIdleForPowerDown(worker.SlurmNode) {
			problems = append(problems, fmt.Sprintf("%s state=%s reason=%s",
				worker.Name, worker.SlurmNode.State, worker.SlurmNode.Reason))
			continue
		}
		candidates = append(candidates, worker)
	}
	if len(candidates) == 0 {
		if len(problems) == 0 {
			return framework.WorkerInfo{}, &framework.InsufficientWorkersError{Label: "static workers", Found: 0, Need: 1}
		}
		sort.Strings(problems)
		return framework.WorkerInfo{}, fmt.Errorf("find an idle static worker ready for work: %s", strings.Join(problems, "; "))
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].HasGPU != candidates[j].HasGPU {
			return !candidates[i].HasGPU
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates[0], nil
}

func isIdleForPowerDown(node framework.SlurmNodeInfo) bool {
	return node.IsReadyForImmediateWork() && node.HasStateFlag("IDLE") && !node.HasStateFlag("COMPLETING")
}

func isPoweringOrPoweredDown(node framework.SlurmNodeInfo) bool {
	return node.HasStateFlag("POWERING_DOWN") || node.HasStateFlag("POWERED_DOWN")
}

func isPowerDownInProgress(node framework.SlurmNodeInfo) bool {
	return node.HasStateFlag("POWER_DOWN") || node.HasStateFlag("POWERING_DOWN")
}

// checkStaticWorkerReturned accepts the worker once a new pod is ready and a fresh slurmd has
// returned the node to service.
func checkStaticWorkerReturned(
	target staticPowerDownTarget,
	pod corev1.Pod,
	found bool,
	node framework.SlurmNodeInfo,
) error {
	switch {
	case !found:
		return fmt.Errorf("worker pod %s is absent", target.podName)
	case string(pod.UID) == target.podUID:
		return fmt.Errorf("worker pod %s still has the original UID %s; Slurm node %s has state=%s",
			target.podName, target.podUID, node.Name, node.State)
	case !kubeobjects.PodReady(pod):
		return fmt.Errorf("recreated worker pod %s (UID %s) is not ready", target.podName, pod.UID)
	case !node.IsReadyForImmediateWork():
		return fmt.Errorf("Slurm node %s has state=%s reason=%s after the pod was recreated",
			node.Name, node.State, node.Reason)
	case !node.SlurmdStartTime.After(target.worker.SlurmNode.SlurmdStartTime):
		return fmt.Errorf("Slurm node %s reports SlurmdStartTime %s, expected later than %s",
			node.Name, node.SlurmdStartTime.Format(time.RFC3339), target.worker.SlurmNode.SlurmdStartTime.Format(time.RFC3339))
	}
	return nil
}

func staticPowerDownCommand(worker string) string {
	return "scontrol power down " + framework.ShellQuote(worker)
}
