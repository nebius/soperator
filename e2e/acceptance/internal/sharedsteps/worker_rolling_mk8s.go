package sharedsteps

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	workerMK8sGroupLabel = "nebius.com/node-group-id"
	workerMK8sTimeout    = 45 * time.Minute
	workerMK8sUpdateStep = "the MK8s node group specification is changed to replace its nodes"
)

type workerRolloutMK8s struct {
	groupID                      string
	originalMaxPods              *mk8sInteger
	originalMaxUnavailable       *workerMK8sLimit
	originalEffectiveUnavailable *workerMK8sLimit
	changedMaxUnavailable        *workerMK8sLimit
	changedMaxPods               mk8sInteger
	specFingerprint              string
	operationID                  string
	requestedMaxPods             *mk8sInteger
}

// The CLI can emit protobuf int64 fields as either numbers or strings.
type mk8sInteger int64

func (n *mk8sInteger) UnmarshalJSON(data []byte) error {
	value := strings.Trim(string(data), "\"")
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid MK8s integer: %w", err)
	}
	*n = mk8sInteger(parsed)
	return nil
}

type workerMK8sGroup struct {
	Metadata struct {
		ID              string      `json:"id"`
		ResourceVersion mk8sInteger `json:"resource_version"`
	} `json:"metadata"`
	Spec struct {
		FixedNodeCount *mk8sInteger           `json:"fixed_node_count"`
		Autoscaling    *workerMK8sAutoscaling `json:"autoscaling"`
		Strategy       *workerMK8sStrategy    `json:"strategy"`
		Template       struct {
			MaxPods *mk8sInteger `json:"max_pods"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		State       string              `json:"state"`
		Reconciling bool                `json:"reconciling"`
		Target      mk8sInteger         `json:"target_node_count"`
		Count       mk8sInteger         `json:"node_count"`
		Ready       mk8sInteger         `json:"ready_node_count"`
		Outdated    mk8sInteger         `json:"outdated_node_count"`
		Strategy    *workerMK8sStrategy `json:"strategy"`
	} `json:"status"`
	fingerprint string
}

type workerMK8sStrategy struct {
	DrainTimeout   string           `json:"drain_timeout"`
	MaxSurge       *workerMK8sLimit `json:"max_surge"`
	MaxUnavailable *workerMK8sLimit `json:"max_unavailable"`
}

type workerMK8sAutoscaling struct {
	MinNodeCount mk8sInteger `json:"min_node_count"`
	MaxNodeCount mk8sInteger `json:"max_node_count"`
}

type workerMK8sLimit struct {
	Count   *mk8sInteger `json:"count"`
	Percent *mk8sInteger `json:"percent"`
}

type workerMK8sOperation struct {
	ID     string `json:"id"`
	Status *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

func (s *WorkerRollingUpdate) registerMK8sSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		s.requireMK8s = false
		for _, step := range scenario.Steps {
			if step.Text == workerMK8sUpdateStep {
				s.requireMK8s = true
				break
			}
		}
		return ctx, nil
	})
	sc.Step(`^`+workerMK8sUpdateStep+`$`, s.changeMK8sGroup)
	sc.Step(`^the original MK8s node group specification is restored$`, s.restoreMK8sGroup)
}

func decodeWorkerMK8sGroup(output string) (workerMK8sGroup, error) {
	var group workerMK8sGroup
	if err := json.Unmarshal([]byte(output), &group); err != nil {
		return group, fmt.Errorf("decode MK8s node group: %w", err)
	}
	if limit := groupMaxUnavailable(group); limit != nil {
		if _, err := workerMK8sLimitCount(*limit, int(group.Status.Target), false); err != nil {
			return group, fmt.Errorf("read MK8s specification max_unavailable: %w", err)
		}
	}
	// Hash configuration without the two fields owned by this scenario. Never
	// include the spec in diagnostics: it can contain cloud-init credentials.
	var raw struct {
		Spec map[string]any `json:"spec"`
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return group, err
	}
	template, ok := raw.Spec["template"].(map[string]any)
	if !ok {
		return group, fmt.Errorf("MK8s node group lacks a template object")
	}
	delete(template, "max_pods")
	if strategy, ok := raw.Spec["strategy"].(map[string]any); ok {
		delete(strategy, "max_unavailable")
		if len(strategy) == 0 {
			delete(raw.Spec, "strategy")
		}
	}
	data, err := json.Marshal(raw.Spec)
	if err != nil {
		return group, err
	}
	group.fingerprint = fmt.Sprintf("%x", sha256.Sum256(data))
	if group.Metadata.ID == "" || group.Metadata.ResourceVersion <= 0 {
		return group, fmt.Errorf("MK8s node group lacks ID or positive resource_version")
	}
	return group, nil
}

func (s *WorkerRollingUpdate) mk8sGroup(ctx context.Context) (workerMK8sGroup, error) {
	output, err := s.runtime.Local().Run(ctx, "nebius", "mk8s", "node-group", "get",
		"--id", s.mk8s.groupID, "--format", "json")
	if err != nil {
		return workerMK8sGroup{}, fmt.Errorf("read MK8s group %s: %w", s.mk8s.groupID, err)
	}
	group, err := decodeWorkerMK8sGroup(output)
	if err != nil {
		return group, err
	}
	if group.Metadata.ID != s.mk8s.groupID {
		return group, fmt.Errorf("MK8s returned group %s, expected %s", group.Metadata.ID, s.mk8s.groupID)
	}
	return group, nil
}

func (s *WorkerRollingUpdate) selectMK8sGroup(ctx context.Context) error {
	var nodes corev1.NodeList
	if err := s.kubectl.GetJSON(ctx, &nodes, "get", "nodes", "-o", "json"); err != nil {
		return err
	}
	groupID, hosts, err := workerMK8sHosts(s.initialPods, nodes.Items)
	if err != nil {
		return err
	}
	s.mk8s.groupID = groupID
	group, err := s.mk8sGroup(ctx)
	if err != nil {
		return err
	}
	if err := workerMK8sGroupReady(group, len(s.initialPods)); err != nil {
		return err
	}
	if group.Status.Count != mk8sInteger(len(hosts)) {
		return fmt.Errorf("match MK8s group %s node snapshot: API reports %d nodes, Kubernetes has %d", groupID, group.Status.Count, len(hosts))
	}
	if group.Status.Strategy == nil || group.Status.Strategy.MaxSurge == nil || group.Status.Strategy.MaxUnavailable == nil {
		return fmt.Errorf("MK8s group %s does not expose effective rollout strategy", groupID)
	}
	surge, err := workerMK8sLimitCount(*group.Status.Strategy.MaxSurge, len(hosts), true)
	if err != nil {
		return err
	}
	unavailable, err := workerMK8sLimitCount(*group.Status.Strategy.MaxUnavailable, len(hosts), false)
	if err != nil {
		return err
	}
	if surge != 0 {
		return workerRolloutPrerequisiteError{fmt.Errorf("MK8s group %s requires zero surge; got surge=%d", groupID, surge)}
	}
	drain, err := time.ParseDuration(group.Status.Strategy.DrainTimeout)
	if err != nil {
		return fmt.Errorf("parse effective MK8s drain timeout: %w", err)
	}
	if drain < 0 || drain > 0 && drain < 10*time.Minute {
		return fmt.Errorf("MK8s group %s drain timeout %s is insufficient for protected worker handoff", groupID, drain)
	}
	changed, err := nextWorkerMK8sMaxPods(group.Spec.Template.MaxPods)
	if err != nil {
		return workerRolloutPrerequisiteError{fmt.Errorf("MK8s group %s has unsupported Pod capacity: %w", groupID, err)}
	}
	var pods corev1.PodList
	if err := s.kubectl.GetJSON(ctx, &pods, "get", "pods", "--all-namespaces", "-o", "json"); err != nil {
		return err
	}
	if err := workerMK8sDedicatedPods(hosts, s.initialPods, pods.Items, int(changed)); err != nil {
		return err
	}
	s.mk8s.originalMaxPods = group.Spec.Template.MaxPods
	if group.Spec.Strategy != nil {
		s.mk8s.originalMaxUnavailable = group.Spec.Strategy.MaxUnavailable
	}
	s.mk8s.originalEffectiveUnavailable = group.Status.Strategy.MaxUnavailable
	s.mk8s.changedMaxUnavailable = s.mk8s.originalMaxUnavailable
	if unavailable < 2 {
		s.mk8s.changedMaxUnavailable = &workerMK8sLimit{Count: new(mk8sInteger(2))}
	}
	s.mk8s.changedMaxPods = changed
	s.mk8s.specFingerprint = group.fingerprint
	return nil
}

// Include idle hosts: an ephemeral NodeSet may populate only part of its
// group, but the MK8s update must replace every original host.
func workerMK8sHosts(workers map[string]workerPodSnapshot, nodes []corev1.Node) (string, map[string]types.UID, error) {
	byName := make(map[string]corev1.Node, len(nodes))
	for _, node := range nodes {
		byName[node.Name] = node
	}
	hosts := make(map[string]types.UID, len(nodes))
	groupID := ""
	for name, pod := range workers {
		node, ok := byName[pod.NodeName]
		if !ok {
			return "", nil, fmt.Errorf("worker %s host %s is absent", name, pod.NodeName)
		}
		id := node.Labels[workerMK8sGroupLabel]
		if id == "" || (groupID != "" && id != groupID) {
			return "", nil, workerRolloutPrerequisiteError{fmt.Errorf("workers do not share one MK8s group")}
		}
		if _, exists := hosts[node.Name]; exists {
			return "", nil, workerRolloutPrerequisiteError{fmt.Errorf("multiple workers occupy Kubernetes node %s", node.Name)}
		}
		groupID = id
		hosts[node.Name] = node.UID
	}
	if groupID == "" {
		return "", nil, fmt.Errorf("selected NodeSet has no worker hosts")
	}
	for _, node := range nodes {
		if node.Labels[workerMK8sGroupLabel] != groupID {
			continue
		}
		if node.UID == "" || node.Spec.Unschedulable || node.DeletionTimestamp != nil || !kubeobjects.NodeReady(node) {
			return "", nil, fmt.Errorf("MK8s host %s is not ready and schedulable", node.Name)
		}
		hosts[node.Name] = node.UID
	}
	return groupID, hosts, nil
}

func workerMK8sDedicatedPods(hosts map[string]types.UID, workers map[string]workerPodSnapshot, pods []corev1.Pod, maxPods int) error {
	counts := make(map[string]int)
	for _, pod := range pods {
		if _, ok := hosts[pod.Spec.NodeName]; !ok || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		counts[pod.Spec.NodeName]++
		if original, ok := workers[pod.Name]; ok && pod.Namespace == framework.SoperatorNamespace && pod.UID == original.UID {
			continue
		}
		if pod.Annotations["kubernetes.io/config.mirror"] != "" {
			continue
		}
		daemon := false
		for _, owner := range pod.OwnerReferences {
			if owner.Kind == "DaemonSet" && owner.Controller != nil && *owner.Controller {
				daemon = true
			}
		}
		if !daemon {
			return workerRolloutPrerequisiteError{fmt.Errorf("MK8s group is not dedicated: unrelated pod %s/%s on %s", pod.Namespace, pod.Name, pod.Spec.NodeName)}
		}
	}
	for host, count := range counts {
		if count >= maxPods {
			return fmt.Errorf("MK8s host %s has %d Pods, leaving no spare Pod slot within proposed capacity %d", host, count, maxPods)
		}
	}
	return nil
}

func workerMK8sLimitCount(limit workerMK8sLimit, size int, roundUp bool) (int, error) {
	if limit.Count != nil && limit.Percent == nil && *limit.Count >= 0 {
		return int(*limit.Count), nil
	}
	if limit.Percent != nil && limit.Count == nil && *limit.Percent >= 0 && *limit.Percent <= 100 {
		count := int(*limit.Percent) * size
		if roundUp {
			count += 99
		}
		return count / 100, nil
	}
	return 0, fmt.Errorf("invalid MK8s rollout count or percentage")
}

func nextWorkerMK8sMaxPods(original *mk8sInteger) (mk8sInteger, error) {
	current := mk8sInteger(110)
	if original != nil && *original != 0 {
		current = *original
	}
	if current < 8 || current > 256 {
		return 0, fmt.Errorf("max_pods=%d is outside 8..256", current)
	}
	if current == 32 {
		return 31, nil
	}
	if current == 31 {
		return 32, nil
	}
	if current == 256 {
		return current - 1, nil
	}
	return current + 1, nil
}

func workerMK8sGroupReady(group workerMK8sGroup, minimumWorkers int) error {
	if (group.Spec.FixedNodeCount == nil) == (group.Spec.Autoscaling == nil) {
		return fmt.Errorf("validate MK8s group %s sizing: require exactly one of fixed_node_count or autoscaling", group.Metadata.ID)
	}
	var minimum, maximum mk8sInteger
	if group.Spec.FixedNodeCount != nil {
		minimum, maximum = *group.Spec.FixedNodeCount, *group.Spec.FixedNodeCount
	} else {
		minimum, maximum = group.Spec.Autoscaling.MinNodeCount, group.Spec.Autoscaling.MaxNodeCount
	}
	if minimum < 0 || maximum < minimum {
		return fmt.Errorf("validate MK8s group %s sizing: invalid node count bounds %d..%d", group.Metadata.ID, minimum, maximum)
	}
	// Autoscaling can remove idle hosts or add capacity during the update. Its
	// current target must converge, while every selected worker still needs a host.
	if minimumWorkers < 1 || group.Status.Target < mk8sInteger(minimumWorkers) ||
		group.Status.Target < minimum || group.Status.Target > maximum ||
		!strings.EqualFold(group.Status.State, "RUNNING") || group.Status.Reconciling || group.Status.Outdated != 0 ||
		group.Status.Count != group.Status.Target || group.Status.Ready != group.Status.Target {
		return fmt.Errorf("MK8s group %s not converged: state=%s reconciling=%t target=%d nodes=%d ready=%d outdated=%d bounds=%d..%d minimum_workers=%d",
			group.Metadata.ID, group.Status.State, group.Status.Reconciling, group.Status.Target, group.Status.Count, group.Status.Ready, group.Status.Outdated, minimum, maximum, minimumWorkers)
	}
	return nil
}

func workerMK8sUpdateArgs(groupID string, version, maxPods mk8sInteger) []string {
	return append(workerMK8sPatchArgs(groupID, version), "--template-max-pods", strconv.FormatInt(int64(maxPods), 10))
}

func groupMaxUnavailable(group workerMK8sGroup) *workerMK8sLimit {
	if group.Spec.Strategy == nil {
		return nil
	}
	return group.Spec.Strategy.MaxUnavailable
}

func sameWorkerMK8sLimit(a, b *workerMK8sLimit) bool {
	return a == nil && b == nil || a != nil && b != nil &&
		sameWorkerMK8sMaxPods(a.Count, b.Count) && sameWorkerMK8sMaxPods(a.Percent, b.Percent)
}

func appendWorkerMK8sLimitArgs(args []string, limit *workerMK8sLimit) []string {
	if limit == nil {
		return append(args, "--clear-mask", "spec.strategy.max_unavailable")
	}
	if limit.Count != nil {
		return append(args, "--strategy-max-unavailable-count", strconv.FormatInt(int64(*limit.Count), 10))
	}
	return append(args, "--strategy-max-unavailable-percent", strconv.FormatInt(int64(*limit.Percent), 10))
}

func workerMK8sPatchArgs(groupID string, version mk8sInteger) []string {
	return []string{"nebius", "mk8s", "node-group", "update", "--id", groupID,
		"--patch", "--resource-version", strconv.FormatInt(int64(version), 10), "--async", "--format", "json"}
}

func restoredWorkerMK8sMaxPods(current, original *mk8sInteger) bool {
	if (current == nil || *current == 0) && (original == nil || *original == 0) {
		return true
	}
	return sameWorkerMK8sMaxPods(current, original)
}

func workerMK8sOperationID(output string) (string, error) {
	var id string
	if json.Unmarshal([]byte(output), &id) == nil && workerMK8sIdentifier(id) {
		return id, nil
	}
	var operation workerMK8sOperation
	if json.Unmarshal([]byte(output), &operation) == nil && workerMK8sIdentifier(operation.ID) {
		return operation.ID, nil
	}
	// Older CLI versions print the async ID as plain text even with --format.
	id = strings.TrimSpace(output)
	if workerMK8sIdentifier(id) {
		return id, nil
	}
	return "", fmt.Errorf("MK8s async update did not return an operation ID")
}

func workerMK8sIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func sameWorkerMK8sMaxPods(a, b *mk8sInteger) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (s *WorkerRollingUpdate) changeMK8sGroup(ctx context.Context) error {
	if s.mk8s.groupID == "" {
		if err := s.selectMK8sGroup(ctx); err != nil {
			var prerequisite workerRolloutPrerequisiteError
			if errors.As(err, &prerequisite) {
				s.runtime.Logf("Acceptance: %v; skipping MK8s rollout", err)
				return godog.ErrSkip
			}
			return err
		}
	}
	if err := s.checkRolloutWorkerSet(ctx); err != nil {
		return err
	}
	group, err := s.mk8sGroup(ctx)
	if err != nil {
		return err
	}
	if group.fingerprint != s.mk8s.specFingerprint || !sameWorkerMK8sMaxPods(group.Spec.Template.MaxPods, s.mk8s.originalMaxPods) ||
		!sameWorkerMK8sLimit(groupMaxUnavailable(group), s.mk8s.originalMaxUnavailable) {
		return fmt.Errorf("MK8s group %s configuration changed after selection", s.mk8s.groupID)
	}
	if err := workerMK8sGroupReady(group, len(s.initialPods)); err != nil {
		return err
	}
	// Log before the request: a timeout can still leave an accepted update running.
	s.runtime.Logf("Update MK8s group %s to max_pods=%d with at least two unavailable nodes; original settings will be restored",
		s.mk8s.groupID, s.mk8s.changedMaxPods)
	// Ownership is recorded before the request, whose response may be lost.
	s.mk8s.requestedMaxPods = new(s.mk8s.changedMaxPods)
	args := workerMK8sUpdateArgs(s.mk8s.groupID, group.Metadata.ResourceVersion, s.mk8s.changedMaxPods)
	if !sameWorkerMK8sLimit(s.mk8s.originalMaxUnavailable, s.mk8s.changedMaxUnavailable) {
		args = appendWorkerMK8sLimitArgs(args, s.mk8s.changedMaxUnavailable)
	}
	output, err := s.runtime.Local().Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("start MK8s group %s rollout to max_pods=%d (cleanup will restore original settings): %w", s.mk8s.groupID, s.mk8s.changedMaxPods, err)
	}
	s.mk8s.operationID, err = workerMK8sOperationID(output)
	if err != nil {
		return fmt.Errorf("read MK8s group %s rollout operation (max_pods=%d may have been accepted): %w", s.mk8s.groupID, s.mk8s.changedMaxPods, err)
	}
	return nil
}

func (s *WorkerRollingUpdate) mk8sConfigurationOwned(group workerMK8sGroup) error {
	if group.fingerprint != s.mk8s.specFingerprint ||
		!restoredWorkerMK8sMaxPods(group.Spec.Template.MaxPods, s.mk8s.originalMaxPods) &&
			!sameWorkerMK8sMaxPods(group.Spec.Template.MaxPods, s.mk8s.requestedMaxPods) ||
		!sameWorkerMK8sLimit(groupMaxUnavailable(group), s.mk8s.originalMaxUnavailable) &&
			!sameWorkerMK8sLimit(groupMaxUnavailable(group), s.mk8s.changedMaxUnavailable) {
		return rolloutInvariant("MK8s group %s configuration changed externally; do not overwrite its specification", s.mk8s.groupID)
	}
	return nil
}

func (s *WorkerRollingUpdate) restoreMK8sGroup(ctx context.Context) error {
	return s.restoreMK8sGroupState(ctx, false)
}

func (s *WorkerRollingUpdate) cleanupMK8sGroup(ctx context.Context) error {
	return s.restoreMK8sGroupState(ctx, true)
}

func (s *WorkerRollingUpdate) restoreMK8sGroupState(ctx context.Context, allowFailedOperation bool) error {
	if s.mk8s.requestedMaxPods == nil {
		return nil
	}
	if err := s.waitForMK8sGroupReady(ctx, allowFailedOperation); err != nil {
		return err
	}
	group, err := s.mk8sGroup(ctx)
	if err != nil {
		return err
	}
	if err := s.mk8sConfigurationOwned(group); err != nil {
		return err
	}
	if restoredWorkerMK8sMaxPods(group.Spec.Template.MaxPods, s.mk8s.originalMaxPods) &&
		sameWorkerMK8sLimit(groupMaxUnavailable(group), s.mk8s.originalMaxUnavailable) {
		s.mk8s.requestedMaxPods = nil
		return nil
	}
	if !restoredWorkerMK8sMaxPods(group.Spec.Template.MaxPods, s.mk8s.originalMaxPods) {
		args := workerMK8sPatchArgs(s.mk8s.groupID, group.Metadata.ResourceVersion)
		if s.mk8s.originalMaxPods == nil {
			args = append(args, "--clear-mask", "spec.template.max_pods")
		} else {
			args = append(args, "--template-max-pods", strconv.FormatInt(int64(*s.mk8s.originalMaxPods), 10))
		}
		s.runtime.Logf("Restore MK8s group %s original Pod capacity while preserving the concurrent rollout budget", s.mk8s.groupID)
		if err := s.startMK8sRestore(ctx, args); err != nil {
			return err
		}
	}
	if err := s.waitForMK8sGroupReady(ctx, allowFailedOperation); err != nil {
		return err
	}
	group, err = s.mk8sGroup(ctx)
	if err != nil {
		return err
	}
	if err := s.mk8sConfigurationOwned(group); err != nil {
		return err
	}
	if !sameWorkerMK8sLimit(groupMaxUnavailable(group), s.mk8s.originalMaxUnavailable) {
		s.runtime.Logf("Restore MK8s group %s original unavailable-node budget", s.mk8s.groupID)
		args := appendWorkerMK8sLimitArgs(workerMK8sPatchArgs(s.mk8s.groupID, group.Metadata.ResourceVersion), s.mk8s.originalMaxUnavailable)
		if err := s.startMK8sRestore(ctx, args); err != nil {
			return err
		}
	}
	if err := s.waitForMK8sGroupReady(ctx, allowFailedOperation); err != nil {
		return err
	}
	group, err = s.mk8sGroup(ctx)
	if err != nil {
		return err
	}
	if err := s.mk8sConfigurationOwned(group); err != nil {
		return err
	}
	if !restoredWorkerMK8sMaxPods(group.Spec.Template.MaxPods, s.mk8s.originalMaxPods) ||
		!sameWorkerMK8sLimit(groupMaxUnavailable(group), s.mk8s.originalMaxUnavailable) {
		return fmt.Errorf("MK8s group %s original Pod capacity and unavailable-node budget are not restored", s.mk8s.groupID)
	}
	s.mk8s.requestedMaxPods = nil
	return nil
}

func (s *WorkerRollingUpdate) startMK8sRestore(ctx context.Context, args []string) error {
	// Clear the previous ID before sending: acceptance may outlive the response.
	s.mk8s.operationID = ""
	output, err := s.runtime.Local().Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("restore MK8s group %s original configuration: %w", s.mk8s.groupID, err)
	}
	s.mk8s.operationID, err = workerMK8sOperationID(output)
	if err != nil {
		return fmt.Errorf("read MK8s group %s restoration operation: %w", s.mk8s.groupID, err)
	}
	return nil
}

func (s *WorkerRollingUpdate) waitForMK8sGroupReady(ctx context.Context, allowFailedOperation bool) error {
	return s.waitForRollout(ctx, "MK8s group and workers healthy during restoration", workerMK8sTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			var nodes corev1.NodeList
			if err := s.kubectl.GetJSON(waitCtx, &nodes, "get", "nodes", "-o", "json"); err != nil {
				return false, err
			}
			if err := s.mk8sGroupAndHostsReady(waitCtx, len(s.workerNames), nodes.Items, allowFailedOperation); err != nil {
				return false, err
			}
			workers, err := s.slurm.NodeInfosOnce(waitCtx, s.workerNames)
			if err != nil {
				return false, err
			}
			if problems := idleProblems(workers); len(problems) > 0 {
				return false, fmt.Errorf("%s", strings.Join(problems, "; "))
			}
			return true, nil
		})
}

func (s *WorkerRollingUpdate) mk8sGroupAndHostsReady(ctx context.Context, minimumWorkers int, nodes []corev1.Node, allowFailedOperation bool) error {
	var operationErr error
	if s.mk8s.operationID != "" {
		completed, err := s.mk8sOperationCompleted(ctx)
		if !completed {
			if err != nil {
				return err
			}
			return fmt.Errorf("MK8s operation %s is still in progress", s.mk8s.operationID)
		}
		if err != nil && !allowFailedOperation {
			return err
		}
		operationErr = err
	}
	group, err := s.mk8sGroup(ctx)
	if err != nil {
		return err
	}
	if err := workerMK8sGroupReady(group, minimumWorkers); err != nil {
		return err
	}
	if err := s.mk8sConfigurationOwned(group); err != nil {
		return err
	}
	wanted := groupMaxUnavailable(group)
	if wanted == nil {
		wanted = s.mk8s.originalEffectiveUnavailable
	}
	if wanted != nil && group.Status.Strategy != nil && group.Status.Strategy.MaxUnavailable != nil {
		expected, err := workerMK8sLimitCount(*wanted, int(group.Status.Target), false)
		if err != nil {
			return err
		}
		actual, err := workerMK8sLimitCount(*group.Status.Strategy.MaxUnavailable, int(group.Status.Target), false)
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("MK8s group %s effective unavailable-node budget is %d, expected %d", s.mk8s.groupID, actual, expected)
		}
	}
	count := 0
	for _, host := range nodes {
		if host.Labels[workerMK8sGroupLabel] != s.mk8s.groupID {
			continue
		}
		count++
		if host.DeletionTimestamp != nil || host.Spec.Unschedulable || !kubeobjects.NodeReady(host) {
			return fmt.Errorf("MK8s group %s host %s not ready and schedulable", s.mk8s.groupID, host.Name)
		}
	}
	if count != int(group.Status.Target) {
		return fmt.Errorf("MK8s group %s has %d Kubernetes hosts, expected %d", s.mk8s.groupID, count, group.Status.Target)
	}
	if operationErr != nil {
		s.runtime.Logf("Cleanup: MK8s operation %s ended unsuccessfully, but group %s and its hosts have recovered: %v", s.mk8s.operationID, s.mk8s.groupID, operationErr)
		s.mk8s.operationID = ""
	}
	return nil
}

func (s *WorkerRollingUpdate) mk8sOperationCompleted(ctx context.Context) (bool, error) {
	if s.mk8s.operationID == "" {
		return true, nil
	}
	output, err := s.runtime.Local().Run(ctx, "nebius", "mk8s", "node-group", "operation", "get", "--id", s.mk8s.operationID, "--format", "json")
	if err != nil {
		return false, err
	}
	return workerMK8sOperationCompleted(s.mk8s.operationID, output)
}

func workerMK8sOperationCompleted(operationID, output string) (bool, error) {
	var operation workerMK8sOperation
	if err := json.Unmarshal([]byte(output), &operation); err != nil {
		return false, fmt.Errorf("decode MK8s operation: %w", err)
	}
	if operation.ID != operationID {
		return false, rolloutInvariant("MK8s returned operation %s, expected %s", operation.ID, operationID)
	}
	if operation.Status != nil && operation.Status.Code != 0 {
		// Do not log the operation request/progress, which can contain user data.
		return true, rolloutInvariant("MK8s operation %s failed with status code %d", operationID, operation.Status.Code)
	}
	return operation.Status != nil, nil
}
