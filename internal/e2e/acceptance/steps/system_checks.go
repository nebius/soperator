package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	systemEphemeralFillPath          = "/tmp/soperator-acceptance-ephemeral-fill"
	systemEphemeralTargetPercent     = 87.0
	systemEphemeralMaxInitialPercent = 75.0
	systemEphemeralDrainTimeout      = 5 * time.Minute
	systemEphemeralRecoverTimeout    = 5 * time.Minute
	systemEphemeralReason            = "[user_problem] pod_ephemeral_storage"
	// Small mirrored image used only to enter the host namespace with kubectl debug.
	systemKubeletDebugImage          = "cr.eu-north1.nebius.cloud/soperator/ubuntu:noble"
	systemKubeletNodeRecreateTimeout = 30 * time.Minute
	systemKubeletWorkerReadyTimeout  = 10 * time.Minute
	systemKubeletSlurmRecoverTimeout = 5 * time.Minute
)

type SystemChecks struct {
	exec  framework.Exec
	slurm *framework.SlurmClient

	worker              framework.WorkerRef
	workerPod           corev1.Pod
	kubeletWorker       framework.WorkerRef
	kubeletK8sNodeName  string
	kubeletK8sNodeUID   string
	kubeletWorkerPodUID string
	kubeletDebugPodName string
}

type workerEphemeralInfo struct {
	UsedBytes    uint64
	LimitBytes   uint64
	UsagePercent float64
}

type kubeletStatsSummary struct {
	Pods []struct {
		PodRef struct {
			UID string `json:"uid"`
		} `json:"podRef"`
		EphemeralStorage struct {
			UsedBytes *uint64 `json:"usedBytes,omitempty"`
		} `json:"ephemeral-storage"`
	} `json:"pods"`
}

var kubectlDebugPodPattern = regexp.MustCompile(`Creating debugging pod ([^\s]+) `)

func NewSystemChecks(exec framework.Exec, slurm *framework.SlurmClient) *SystemChecks {
	return &SystemChecks{
		exec:  exec,
		slurm: slurm,
	}
}

func (s *SystemChecks) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		s.cleanup(context.Background())
		return ctx, nil
	})

	sc.Step(`^a healthy worker pod is selected$`, s.aHealthyWorkerPodIsSelected)
	sc.Step(`^pod-local ephemeral storage is filled above the warning threshold$`, s.podLocalEphemeralStorageIsFilledAboveTheWarningThreshold)
	sc.Step(`^the selected worker is drained by pod_ephemeral_storage$`, s.theSelectedWorkerIsDrainedByPodEphemeralStorage)
	sc.Step(`^the pod-local ephemeral storage fill file is removed$`, s.thePodLocalEphemeralStorageFillFileIsRemoved)
	sc.Step(`^the selected worker no longer has pod_ephemeral_storage reason$`, s.theSelectedWorkerNoLongerHasPodEphemeralStorageReason)
	sc.Step(`^the selected worker is usable after pod_ephemeral_storage$`, s.theSelectedWorkerIsUsableAfterPodEphemeralStorage)
	sc.Step(`^kubelet is stopped on the selected worker Kubernetes node$`, s.kubeletIsStoppedOnTheSelectedWorkerKubernetesNode)
	sc.Step(`^the selected worker Kubernetes node is recreated$`, s.theSelectedWorkerKubernetesNodeIsRecreated)
	sc.Step(`^the selected worker pod is recreated and ready$`, s.theSelectedWorkerPodIsRecreatedAndReady)
	sc.Step(`^the selected Slurm worker is present after kubelet replacement$`, s.theSelectedSlurmWorkerIsPresentAfterKubeletReplacement)
	sc.Step(`^the selected Slurm worker is usable after kubelet replacement$`, s.theSelectedSlurmWorkerIsUsableAfterKubeletReplacement)
}

func (s *SystemChecks) aHealthyWorkerPodIsSelected(ctx context.Context) error {
	var problems []string
	for _, worker := range s.exec.AvailableWorkers(framework.WorkerAny) {
		node, err := s.slurm.NodeInfo(ctx, worker.Name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", worker.Name, err))
			continue
		}
		if !node.IsUsable() {
			problems = append(problems, fmt.Sprintf("%s: Slurm node is not usable", worker.Name))
			continue
		}
		if worker.PodName == "" {
			problems = append(problems, fmt.Sprintf("%s: Kubernetes worker pod was not discovered", worker.Name))
			continue
		}

		pod, err := s.workerPodByName(ctx, worker.PodName)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", worker.Name, err))
			continue
		}
		if pod.Status.Phase != corev1.PodRunning || !podReady(pod) || pod.Spec.NodeName == "" {
			problems = append(problems, fmt.Sprintf("%s: pod phase=%s ready=%t node=%q", worker.Name, pod.Status.Phase, podReady(pod), pod.Spec.NodeName))
			continue
		}
		k8sNode, err := s.k8sNodeByName(ctx, pod.Spec.NodeName)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", worker.Name, err))
			continue
		}
		if !k8sNodeReady(k8sNode) {
			problems = append(problems, fmt.Sprintf("%s: Kubernetes node %s is not Ready", worker.Name, k8sNode.Name))
			continue
		}

		s.worker = worker
		s.workerPod = pod
		s.exec.Logf("system checks: selected worker=%s pod=%s k8s_node=%s",
			s.worker.Name, pod.Name, pod.Spec.NodeName)
		return nil
	}
	return fmt.Errorf("no healthy worker pod found: %s", strings.Join(problems, "; "))
}

func (s *SystemChecks) podLocalEphemeralStorageIsFilledAboveTheWarningThreshold(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	info, err := s.ephemeralInfo(ctx, s.workerPod)
	if err != nil {
		return err
	}
	if info.UsagePercent >= systemEphemeralMaxInitialPercent {
		return fmt.Errorf("current ephemeral usage %.2f%% is too high for bounded test", info.UsagePercent)
	}
	targetUsed := uint64(float64(info.LimitBytes) * systemEphemeralTargetPercent / 100.0)
	if targetUsed <= info.UsedBytes {
		return fmt.Errorf("current ephemeral usage %.2f%% is already at or above target %.2f%%", info.UsagePercent, systemEphemeralTargetPercent)
	}
	fillSizeByte := targetUsed - info.UsedBytes

	cmd := fmt.Sprintf("rm -f %s && fallocate --length %d %s && ls -lh %s",
		framework.ShellQuote(systemEphemeralFillPath),
		fillSizeByte,
		framework.ShellQuote(systemEphemeralFillPath),
		framework.ShellQuote(systemEphemeralFillPath),
	)
	if _, err := s.exec.WorkerPod(s.worker).Run(ctx, cmd); err != nil {
		return fmt.Errorf("fill ephemeral storage in worker pod %s: %w", s.workerPod.Name, err)
	}
	s.exec.Logf("system checks: filled %d bytes in %s:%s", fillSizeByte, s.workerPod.Name, systemEphemeralFillPath)
	return nil
}

func (s *SystemChecks) theSelectedWorkerIsDrainedByPodEphemeralStorage(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	return s.slurm.WaitForNodeReasonContains(ctx, s.worker.Name, systemEphemeralReason, systemEphemeralDrainTimeout)
}

func (s *SystemChecks) thePodLocalEphemeralStorageFillFileIsRemoved(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	if _, err := s.exec.WorkerPod(s.worker).Run(ctx, fmt.Sprintf("rm -f %s", framework.ShellQuote(systemEphemeralFillPath))); err != nil {
		return fmt.Errorf("remove ephemeral fill file from worker pod %s: %w", s.workerPod.Name, err)
	}
	return nil
}

func (s *SystemChecks) theSelectedWorkerNoLongerHasPodEphemeralStorageReason(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	return s.slurm.WaitForNodeReasonCleared(ctx, s.worker.Name, systemEphemeralReason, systemEphemeralRecoverTimeout)
}

func (s *SystemChecks) theSelectedWorkerIsUsableAfterPodEphemeralStorage(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	if err := s.slurm.WaitForNodeUsable(ctx, s.worker.Name, systemEphemeralRecoverTimeout); err != nil {
		return err
	}
	s.worker = framework.WorkerRef{}
	s.workerPod = corev1.Pod{}
	return nil
}

func (s *SystemChecks) kubeletIsStoppedOnTheSelectedWorkerKubernetesNode(ctx context.Context) error {
	if s.worker.Name == "" || s.workerPod.Name == "" {
		return fmt.Errorf("worker pod is not selected")
	}
	pod, err := s.workerPodByName(ctx, s.worker.PodName)
	if err != nil {
		return err
	}
	if pod.Status.Phase != corev1.PodRunning || !podReady(pod) || pod.Spec.NodeName == "" {
		return fmt.Errorf("selected worker pod %s is not healthy: phase=%s ready=%t node=%q",
			pod.Name, pod.Status.Phase, podReady(pod), pod.Spec.NodeName)
	}
	k8sNode, err := s.k8sNodeByName(ctx, pod.Spec.NodeName)
	if err != nil {
		return err
	}
	if !k8sNodeReady(k8sNode) {
		return fmt.Errorf("selected Kubernetes node %s is not Ready", k8sNode.Name)
	}
	s.kubeletWorker = s.worker
	s.kubeletK8sNodeName = k8sNode.Name
	s.kubeletK8sNodeUID = string(k8sNode.UID)
	s.kubeletWorkerPodUID = string(pod.UID)
	s.exec.Logf("system checks: kubelet target worker=%s pod=%s pod_uid=%s k8s_node=%s k8s_node_uid=%s",
		s.kubeletWorker.Name, pod.Name, s.kubeletWorkerPodUID, s.kubeletK8sNodeName, s.kubeletK8sNodeUID)

	out, err := s.exec.Kubectl().Run(ctx,
		"debug", "node/"+s.kubeletK8sNodeName,
		"--image="+systemKubeletDebugImage,
		"--profile=sysadmin",
		"--", "chroot", "/host", "systemctl", "stop", "kubelet.service",
	)
	if err != nil {
		return fmt.Errorf("stop kubelet on Kubernetes node %s: %w", s.kubeletK8sNodeName, err)
	}
	s.kubeletDebugPodName = parseKubectlDebugPodName(out)
	if s.kubeletDebugPodName != "" {
		s.exec.Logf("system checks: kubelet stop debug pod=%s", s.kubeletDebugPodName)
	}
	return nil
}

func (s *SystemChecks) theSelectedWorkerKubernetesNodeIsRecreated(ctx context.Context) error {
	if s.kubeletK8sNodeName == "" || s.kubeletK8sNodeUID == "" {
		return fmt.Errorf("worker Kubernetes node is not captured")
	}
	return s.exec.WaitFor(ctx, fmt.Sprintf("Kubernetes node %s recreated", s.kubeletK8sNodeName), systemKubeletNodeRecreateTimeout, 30*time.Second, func(waitCtx context.Context) (bool, error) {
		node, err := s.k8sNodeByName(waitCtx, s.kubeletK8sNodeName)
		if err != nil {
			return false, err
		}
		return string(node.UID) != s.kubeletK8sNodeUID && k8sNodeReady(node), nil
	})
}

func (s *SystemChecks) theSelectedWorkerPodIsRecreatedAndReady(ctx context.Context) error {
	if s.kubeletWorker.PodName == "" || s.kubeletWorkerPodUID == "" {
		return fmt.Errorf("worker pod is not captured")
	}
	return s.exec.WaitFor(ctx, fmt.Sprintf("worker pod %s recreated and ready", s.kubeletWorker.PodName), systemKubeletWorkerReadyTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		pod, err := s.workerPodByName(waitCtx, s.kubeletWorker.PodName)
		if err != nil {
			return false, err
		}
		return string(pod.UID) != s.kubeletWorkerPodUID && pod.Status.Phase == corev1.PodRunning && podReady(pod), nil
	})
}

func (s *SystemChecks) theSelectedSlurmWorkerIsPresentAfterKubeletReplacement(ctx context.Context) error {
	if s.kubeletWorker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	return s.exec.WaitFor(ctx, fmt.Sprintf("Slurm node %s present after kubelet replacement", s.kubeletWorker.Name), systemKubeletSlurmRecoverTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		_, err := s.slurm.NodeInfoOnce(waitCtx, s.kubeletWorker.Name)
		ready := err == nil
		return ready, nil
	})
}

func (s *SystemChecks) theSelectedSlurmWorkerIsUsableAfterKubeletReplacement(ctx context.Context) error {
	if s.kubeletWorker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	if err := s.exec.WaitFor(ctx, fmt.Sprintf("Slurm node %s usable after kubelet replacement", s.kubeletWorker.Name), systemKubeletSlurmRecoverTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := s.slurm.NodeInfo(waitCtx, s.kubeletWorker.Name)
		if err != nil {
			return false, err
		}
		return node.IsUsable(), nil
	}); err != nil {
		return err
	}
	s.kubeletWorker = framework.WorkerRef{}
	s.kubeletK8sNodeName = ""
	s.kubeletK8sNodeUID = ""
	s.kubeletWorkerPodUID = ""
	return nil
}

func (s *SystemChecks) cleanup(ctx context.Context) {
	if s.kubeletDebugPodName != "" {
		if _, err := s.exec.Kubectl().Run(ctx, "delete", "pod", s.kubeletDebugPodName, "--ignore-not-found"); err != nil {
			s.exec.Logf("cleanup: delete kubelet debug pod %s: %v", s.kubeletDebugPodName, err)
		} else {
			s.kubeletDebugPodName = ""
		}
	}
	if s.workerPod.Name != "" {
		if _, err := s.exec.WorkerPod(s.worker).Run(ctx, fmt.Sprintf("rm -f %s >/dev/null 2>&1 || true", framework.ShellQuote(systemEphemeralFillPath))); err != nil {
			s.exec.Logf("cleanup: remove ephemeral fill file from %s: %v", s.workerPod.Name, err)
		} else {
			s.workerPod = corev1.Pod{}
		}
	}
	if s.worker.Name != "" {
		if err := s.slurm.ResumeNodeIfDrainedByReason(ctx, s.worker.Name, systemEphemeralReason); err != nil {
			s.exec.Logf("cleanup: resume %s after pod_ephemeral_storage: %v", s.worker.Name, err)
		} else {
			s.worker = framework.WorkerRef{}
		}
	}
}

func (s *SystemChecks) ephemeralInfo(ctx context.Context, pod corev1.Pod) (workerEphemeralInfo, error) {
	limitBytes := podEphemeralLimitBytes(pod)
	if limitBytes == 0 {
		return workerEphemeralInfo{}, fmt.Errorf("pod %s has no ephemeral-storage limit", pod.Name)
	}

	raw, err := s.exec.Kubectl().RunWithDefaultRetry(ctx, "get", "--raw", fmt.Sprintf("/api/v1/nodes/%s/proxy/stats/summary", pod.Spec.NodeName))
	if err != nil {
		return workerEphemeralInfo{}, fmt.Errorf("query kubelet stats for node %s: %w", pod.Spec.NodeName, err)
	}
	var stats kubeletStatsSummary
	if err := json.Unmarshal([]byte(raw), &stats); err != nil {
		return workerEphemeralInfo{}, fmt.Errorf("decode kubelet stats for node %s: %w", pod.Spec.NodeName, err)
	}

	for _, podStats := range stats.Pods {
		if podStats.PodRef.UID != string(pod.UID) {
			continue
		}
		var usedBytes uint64
		if podStats.EphemeralStorage.UsedBytes != nil {
			usedBytes = *podStats.EphemeralStorage.UsedBytes
		}
		return workerEphemeralInfo{
			UsedBytes:    usedBytes,
			LimitBytes:   limitBytes,
			UsagePercent: float64(usedBytes) / float64(limitBytes) * 100.0,
		}, nil
	}
	return workerEphemeralInfo{}, fmt.Errorf("kubelet stats for pod %s/%s were not found", pod.Namespace, pod.Name)
}

func (s *SystemChecks) workerPodByName(ctx context.Context, name string) (corev1.Pod, error) {
	var pod corev1.Pod
	if err := kubectlJSON(ctx, s.exec, &pod, "get", "pod", "-n", clusterCreationNamespace, name, "-o", "json"); err != nil {
		return corev1.Pod{}, fmt.Errorf("get worker pod %s: %w", name, err)
	}
	return pod, nil
}

func (s *SystemChecks) k8sNodeByName(ctx context.Context, name string) (corev1.Node, error) {
	var node corev1.Node
	if err := kubectlJSON(ctx, s.exec, &node, "get", "node", name, "-o", "json"); err != nil {
		return corev1.Node{}, fmt.Errorf("get Kubernetes node %s: %w", name, err)
	}
	return node, nil
}

func k8sNodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func parseKubectlDebugPodName(output string) string {
	match := kubectlDebugPodPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func podEphemeralLimitBytes(pod corev1.Pod) uint64 {
	var total int64
	for _, container := range pod.Spec.Containers {
		if limit, ok := container.Resources.Limits[corev1.ResourceEphemeralStorage]; ok {
			total += limit.Value()
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if limit, ok := container.Resources.Limits[corev1.ResourceEphemeralStorage]; ok {
			total += limit.Value()
		}
	}
	if total <= 0 {
		return 0
	}
	return uint64(total)
}
