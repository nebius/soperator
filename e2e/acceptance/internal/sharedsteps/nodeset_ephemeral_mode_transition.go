package sharedsteps

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	nodeSetEphemeralModeTransitionTimeout = 15 * time.Minute
	nodeSetEphemeralModeCleanupTimeout    = 5 * time.Minute
	nodeSetLabelKey                       = "slurm.nebius.ai/nodeset"
	ephemeralModeApplied                  = "EphemeralModeApplied"
)

type NodeSetEphemeralModeTransition struct {
	info    *framework.ClusterInfo
	runtime framework.Runtime
	kubectl *framework.KubectlClient

	initialNodeSet kubeobjects.NodeSet
	initialPods    map[string]string
}

func NewNodeSetEphemeralModeTransition(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	kubectl *framework.KubectlClient,
) *NodeSetEphemeralModeTransition {
	return &NodeSetEphemeralModeTransition{
		info:    info,
		runtime: runtime,
		kubectl: kubectl,
	}
}

func (s *NodeSetEphemeralModeTransition) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a ready static NodeSet is selected for a mode transition$`, s.selectReadyStaticNodeSet)
	sc.Step(`^the selected NodeSet is switched to ephemeral mode$`, s.switchToEphemeralMode)
	sc.Step(`^all requested workers remain active without pod recreation$`, s.checkWorkersPreserved)
	sc.Step(`^the selected NodeSet is switched back to static mode$`, s.switchToStaticMode)
	sc.Step(`^its power state is removed and all static workers are ready$`, s.checkStaticModeRestored)
}

func (s *NodeSetEphemeralModeTransition) CleanupAndReset(ctx context.Context) {
	if s.initialNodeSet.Metadata.Name != "" {
		cleanupCtx, cancel := context.WithTimeout(ctx, nodeSetEphemeralModeCleanupTimeout)
		defer cancel()
		if err := s.patchNodeSetMode(cleanupCtx, false, s.initialNodeSet.Spec.InitialNumberEphemeralNodes); err != nil {
			s.runtime.Logf("cleanup: restore static NodeSet %s/%s: %v",
				s.initialNodeSet.Metadata.Namespace, s.initialNodeSet.Metadata.Name, err)
		} else if err := s.checkStaticModeRestored(cleanupCtx); err != nil {
			s.runtime.Logf("cleanup: wait for static NodeSet %s/%s: %v",
				s.initialNodeSet.Metadata.Namespace, s.initialNodeSet.Metadata.Name, err)
		}
	}
	s.initialNodeSet = kubeobjects.NodeSet{}
	s.initialPods = nil
}

func (s *NodeSetEphemeralModeTransition) selectReadyStaticNodeSet(ctx context.Context) error {
	var nodeSets kubeobjects.NodeSetList
	if err := s.kubectl.GetJSON(ctx, &nodeSets, "get", "nodesets", "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("list NodeSets: %w", err)
	}
	sort.Slice(nodeSets.Items, func(i, j int) bool {
		return nodeSets.Items[i].Metadata.Name < nodeSets.Items[j].Metadata.Name
	})

	for _, nodeSet := range nodeSets.Items {
		if nodeSet.Spec.ClusterName != "" && nodeSet.Spec.ClusterName != s.info.SlurmClusterName {
			continue
		}
		if nodeSet.Spec.EphemeralNodes != nil && *nodeSet.Spec.EphemeralNodes {
			continue
		}
		if nodeSet.Spec.Replicas == 0 || nodeSet.Status.Replicas != nodeSet.Spec.Replicas {
			continue
		}
		s.initialNodeSet = nodeSet
		break
	}
	if s.initialNodeSet.Metadata.Name == "" {
		s.runtime.Logf("acceptance: no ready static NodeSet with replicas was found, skipping scenario")
		return godog.ErrSkip
	}
	if err := s.waitForModeApplied(ctx, metav1.ConditionFalse); err != nil {
		return fmt.Errorf("wait for selected NodeSet static mode: %w", err)
	}

	pods, err := s.workerPods(ctx)
	if err != nil {
		return err
	}
	if int32(len(pods)) != s.initialNodeSet.Spec.Replicas {
		return fmt.Errorf("NodeSet %s/%s has %d worker pods, expected %d",
			s.initialNodeSet.Metadata.Namespace, s.initialNodeSet.Metadata.Name, len(pods), s.initialNodeSet.Spec.Replicas)
	}
	s.initialPods = make(map[string]string, len(pods))
	for _, pod := range pods {
		if !kubeobjects.PodReady(pod) {
			return fmt.Errorf("worker pod %s is not ready", pod.Name)
		}
		s.initialPods[pod.Name] = string(pod.UID)
	}
	return nil
}

func (s *NodeSetEphemeralModeTransition) switchToEphemeralMode(ctx context.Context) error {
	return s.patchNodeSetMode(ctx, true, 0)
}

func (s *NodeSetEphemeralModeTransition) switchToStaticMode(ctx context.Context) error {
	return s.patchNodeSetMode(ctx, false, s.initialNodeSet.Spec.InitialNumberEphemeralNodes)
}

func (s *NodeSetEphemeralModeTransition) patchNodeSetMode(ctx context.Context, enabled bool, initialEphemeralNodes int32) error {
	patch := fmt.Sprintf(
		`{"spec":{"ephemeralNodes":%t,"initialNumberEphemeralNodes":%d}}`,
		enabled,
		initialEphemeralNodes,
	)
	if _, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
		"patch", "nodeset", s.initialNodeSet.Metadata.Name,
		"-n", s.initialNodeSet.Metadata.Namespace,
		"--type=merge", "-p", patch,
	); err != nil {
		return fmt.Errorf("patch NodeSet mode: %w", err)
	}
	return nil
}

func (s *NodeSetEphemeralModeTransition) checkWorkersPreserved(ctx context.Context) error {
	if err := s.waitForModeApplied(ctx, metav1.ConditionTrue); err != nil {
		return err
	}

	wantActiveNodes := make([]int32, s.initialNodeSet.Spec.Replicas)
	for ordinal := range s.initialNodeSet.Spec.Replicas {
		wantActiveNodes[ordinal] = ordinal
	}
	if err := s.runtime.WaitFor(ctx, "all requested NodeSet ordinals to become active",
		nodeSetEphemeralModeTransitionTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			var powerState kubeobjects.NodeSetPowerState
			if err := s.kubectl.GetJSON(waitCtx, &powerState,
				"get", "nodesetpowerstate", s.initialNodeSet.Metadata.Name,
				"-n", s.initialNodeSet.Metadata.Namespace, "-o", "json",
			); err != nil {
				return false, err
			}
			return slices.Equal(powerState.Spec.ActiveNodes, wantActiveNodes), nil
		},
	); err != nil {
		return err
	}

	return s.runtime.WaitFor(ctx, "original worker pods to remain ready",
		nodeSetEphemeralModeTransitionTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			pods, err := s.workerPods(waitCtx)
			if err != nil {
				return false, err
			}
			if len(pods) != len(s.initialPods) {
				return false, fmt.Errorf("found %d worker pods, expected %d", len(pods), len(s.initialPods))
			}
			for _, pod := range pods {
				uid, exists := s.initialPods[pod.Name]
				if !exists {
					return false, fmt.Errorf("unexpected worker pod %s", pod.Name)
				}
				if string(pod.UID) != uid {
					return false, fmt.Errorf("worker pod %s was recreated", pod.Name)
				}
				if !kubeobjects.PodReady(pod) {
					return false, nil
				}
			}
			return true, nil
		},
	)
}

func (s *NodeSetEphemeralModeTransition) checkStaticModeRestored(ctx context.Context) error {
	if err := s.waitForModeApplied(ctx, metav1.ConditionFalse); err != nil {
		return err
	}
	if err := s.runtime.WaitFor(ctx, "NodeSetPowerState removal",
		nodeSetEphemeralModeTransitionTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			out, err := s.runtime.Kubectl().Run(waitCtx,
				"get", "nodesetpowerstate", s.initialNodeSet.Metadata.Name,
				"-n", s.initialNodeSet.Metadata.Namespace,
				"--ignore-not-found", "-o", "name",
			)
			return err == nil && strings.TrimSpace(out) == "", err
		},
	); err != nil {
		return err
	}

	if err := s.runtime.WaitFor(ctx, "static StatefulSet replicas",
		nodeSetEphemeralModeTransitionTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			var statefulSet kubeobjects.KruiseStatefulSet
			if err := s.kubectl.GetJSON(waitCtx, &statefulSet,
				"get", "statefulsets.apps.kruise.io", s.initialNodeSet.Metadata.Name,
				"-n", s.initialNodeSet.Metadata.Namespace, "-o", "json",
			); err != nil {
				return false, err
			}
			return statefulSet.Spec.Replicas != nil &&
				*statefulSet.Spec.Replicas == s.initialNodeSet.Spec.Replicas &&
				len(statefulSet.Spec.ReserveOrdinals) == 0, nil
		},
	); err != nil {
		return err
	}

	return s.runtime.WaitFor(ctx, "all contiguous static worker pods to become ready",
		nodeSetEphemeralModeTransitionTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			pods, err := s.workerPods(waitCtx)
			if err != nil {
				return false, err
			}
			if int32(len(pods)) != s.initialNodeSet.Spec.Replicas {
				return false, nil
			}
			podsByName := make(map[string]corev1.Pod, len(pods))
			for _, pod := range pods {
				podsByName[pod.Name] = pod
			}
			for ordinal := range s.initialNodeSet.Spec.Replicas {
				podName := fmt.Sprintf("%s-%d", s.initialNodeSet.Metadata.Name, ordinal)
				pod, exists := podsByName[podName]
				if !exists || !kubeobjects.PodReady(pod) {
					return false, nil
				}
			}
			return true, nil
		},
	)
}

func (s *NodeSetEphemeralModeTransition) waitForModeApplied(ctx context.Context, status metav1.ConditionStatus) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("EphemeralModeApplied=%s", status),
		nodeSetEphemeralModeTransitionTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			var nodeSet kubeobjects.NodeSet
			if err := s.kubectl.GetJSON(waitCtx, &nodeSet,
				"get", "nodeset", s.initialNodeSet.Metadata.Name,
				"-n", s.initialNodeSet.Metadata.Namespace, "-o", "json",
			); err != nil {
				return false, err
			}
			for _, condition := range nodeSet.Status.Conditions {
				if condition.Type == ephemeralModeApplied &&
					condition.Status == status &&
					condition.ObservedGeneration == nodeSet.Metadata.Generation {
					return true, nil
				}
			}
			return false, nil
		},
	)
}

func (s *NodeSetEphemeralModeTransition) workerPods(ctx context.Context) ([]corev1.Pod, error) {
	var pods corev1.PodList
	selector := fmt.Sprintf("%s=%s", nodeSetLabelKey, s.initialNodeSet.Metadata.Name)
	if err := s.kubectl.GetJSON(ctx, &pods,
		"get", "pods", "-n", s.initialNodeSet.Metadata.Namespace,
		"-l", selector, "-o", "json",
	); err != nil {
		return nil, fmt.Errorf("list worker pods for NodeSet %s: %w", s.initialNodeSet.Metadata.Name, err)
	}
	return pods.Items, nil
}
