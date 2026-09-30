package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	systemEphemeralFillPath          = "/tmp/soperator-acceptance-ephemeral-fill"
	systemEphemeralTargetPercent     = 87.0
	systemEphemeralMaxInitialPercent = 75.0
	systemEphemeralDrainTimeout      = 5 * time.Minute
	systemEphemeralRecoverTimeout    = 5 * time.Minute
	systemEphemeralReason            = "[user_problem] pod_ephemeral_storage"
	// Small mirrored image used only to enter the host namespace with kubectl debug.
	systemKubeletDebugImage          = "cr.nebius.cloud/soperator/ubuntu:noble"
	systemKubeletNodeRecreateTimeout = 30 * time.Minute
	systemKubeletWorkerReadyTimeout  = 10 * time.Minute
	systemKubeletSlurmRecoverTimeout = 5 * time.Minute
	systemWorkerRestartTimeout       = 2 * time.Minute
	libslurmProbeStartTimeout        = 10 * time.Second
	libslurmProbeStopTimeout         = 5 * time.Second
)

type libslurmProbeResult struct {
	output string
	err    error
}

type SystemChecks struct {
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	worker               framework.WorkerInfo
	workerPodInfo        framework.WorkerPodInfo
	workerPod            corev1.Pod
	kubeletWorker        framework.WorkerInfo
	kubeletWorkerPodName string
	kubeletK8sNodeName   string
	kubeletK8sNodeUID    string
	kubeletWorkerPodUID  string
	kubeletDebugPodName  string

	restartedWorkerPodUID string
	libslurmProbeCancel   context.CancelFunc
	libslurmProbeResults  <-chan libslurmProbeResult
	libslurmProbeFinished *libslurmProbeResult
	libslurmProbeReady    string
	libslurmProbeFailure  string
	libslurmProbePID      string
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

func NewSystemChecks(runtime framework.Runtime, slurm *framework.SlurmClient, kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *SystemChecks {
	return &SystemChecks{
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *SystemChecks) RegisterSteps(sc *godog.ScenarioContext) {
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
	sc.Step(`^the shared libslurm symlinks are continuously monitored from login$`, s.theSharedLibslurmSymlinksAreContinuouslyMonitoredFromLogin)
	sc.Step(`^the selected worker pod is restarted$`, s.theSelectedWorkerPodIsRestarted)
	sc.Step(`^the restarted worker pod is ready$`, s.theRestartedWorkerPodIsReady)
	sc.Step(`^the shared libslurm symlinks remained continuously readable$`, s.theSharedLibslurmSymlinksRemainedContinuouslyReadable)
}

func (s *SystemChecks) CleanupAndReset(ctx context.Context) {
	s.stopLibslurmProbe(ctx)
	s.removeLibslurmProbeFiles(ctx)
	if s.kubeletDebugPodName != "" {
		if _, err := s.runtime.Kubectl().Run(ctx, "delete", "pod", "-n", framework.SoperatorNamespace, s.kubeletDebugPodName, "--ignore-not-found"); err != nil {
			s.runtime.Logf("cleanup: delete kubelet debug pod %s: %v", s.kubeletDebugPodName, err)
		}
	}
	if s.workerPod.Name != "" {
		if _, err := s.runtime.WorkerPod(s.workerPodInfo).Run(ctx, fmt.Sprintf("rm -f %s >/dev/null 2>&1 || true", framework.ShellQuote(systemEphemeralFillPath))); err != nil {
			s.runtime.Logf("cleanup: remove ephemeral fill file from %s: %v", s.workerPod.Name, err)
		}
	}
	if s.worker.Name != "" {
		if err := s.slurm.ResumeNodeIfDrainedByReason(ctx, s.worker.Name, systemEphemeralReason); err != nil {
			s.runtime.Logf("cleanup: resume %s after pod_ephemeral_storage: %v", s.worker.Name, err)
		}
	}
	s.worker = framework.WorkerInfo{}
	s.workerPodInfo = framework.WorkerPodInfo{}
	s.workerPod = corev1.Pod{}
	s.kubeletWorker = framework.WorkerInfo{}
	s.kubeletWorkerPodName = ""
	s.kubeletK8sNodeName = ""
	s.kubeletK8sNodeUID = ""
	s.kubeletWorkerPodUID = ""
	s.kubeletDebugPodName = ""
	s.restartedWorkerPodUID = ""
	s.resetLibslurmProbe()
}

func (s *SystemChecks) aHealthyWorkerPodIsSelected(ctx context.Context) error {
	workers, err := s.selector.Workers(ctx)
	if err != nil {
		return err
	}
	var problems []string
	for _, worker := range workers {
		node, err := s.slurm.NodeInfo(ctx, worker.Name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", worker.Name, err))
			continue
		}
		if !node.IsUsable() {
			problems = append(problems, fmt.Sprintf("%s: Slurm node is not usable", worker.Name))
			continue
		}

		podInfo, err := s.kubectl.WorkerPodForSlurmNode(ctx, worker.Name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", worker.Name, err))
			continue
		}
		pod, err := s.workerPodByName(ctx, podInfo.PodName)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", worker.Name, err))
			continue
		}
		if pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(pod) || pod.Spec.NodeName == "" {
			problems = append(problems, fmt.Sprintf("%s: pod phase=%s ready=%t node=%q", worker.Name, pod.Status.Phase, kubeobjects.PodReady(pod), pod.Spec.NodeName))
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
		s.workerPodInfo = podInfo
		s.workerPod = pod
		s.runtime.Logf("system checks: selected worker=%s pod=%s k8s_node=%s",
			s.worker.Name, pod.Name, pod.Spec.NodeName)
		return nil
	}
	if len(problems) == 0 {
		return fmt.Errorf("no usable workers found")
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
	if _, err := s.runtime.WorkerPod(s.workerPodInfo).Run(ctx, cmd); err != nil {
		return fmt.Errorf("fill ephemeral storage in worker pod %s: %w", s.workerPod.Name, err)
	}
	s.runtime.Logf("system checks: filled %d bytes in %s:%s", fillSizeByte, s.workerPod.Name, systemEphemeralFillPath)
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
	if _, err := s.runtime.WorkerPod(s.workerPodInfo).Run(ctx, fmt.Sprintf("rm -f %s", framework.ShellQuote(systemEphemeralFillPath))); err != nil {
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
	s.worker = framework.WorkerInfo{}
	s.workerPodInfo = framework.WorkerPodInfo{}
	s.workerPod = corev1.Pod{}
	return nil
}

func (s *SystemChecks) kubeletIsStoppedOnTheSelectedWorkerKubernetesNode(ctx context.Context) error {
	if s.worker.Name == "" || s.workerPodInfo.PodName == "" {
		return fmt.Errorf("worker pod is not selected")
	}
	pod, err := s.workerPodByName(ctx, s.workerPodInfo.PodName)
	if err != nil {
		return err
	}
	if pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(pod) || pod.Spec.NodeName == "" {
		return fmt.Errorf("selected worker pod %s is not healthy: phase=%s ready=%t node=%q",
			pod.Name, pod.Status.Phase, kubeobjects.PodReady(pod), pod.Spec.NodeName)
	}
	k8sNode, err := s.k8sNodeByName(ctx, pod.Spec.NodeName)
	if err != nil {
		return err
	}
	if !k8sNodeReady(k8sNode) {
		return fmt.Errorf("selected Kubernetes node %s is not Ready", k8sNode.Name)
	}
	s.kubeletWorker = s.worker
	s.kubeletWorkerPodName = pod.Name
	s.kubeletK8sNodeName = k8sNode.Name
	s.kubeletK8sNodeUID = string(k8sNode.UID)
	s.kubeletWorkerPodUID = string(pod.UID)
	s.runtime.Logf("system checks: kubelet target worker=%s pod=%s pod_uid=%s k8s_node=%s k8s_node_uid=%s",
		s.kubeletWorker.Name, pod.Name, s.kubeletWorkerPodUID, s.kubeletK8sNodeName, s.kubeletK8sNodeUID)

	out, err := s.runtime.Kubectl().Run(ctx,
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
		s.runtime.Logf("system checks: kubelet stop debug pod=%s", s.kubeletDebugPodName)
	}
	return nil
}

func (s *SystemChecks) theSelectedWorkerKubernetesNodeIsRecreated(ctx context.Context) error {
	if s.kubeletK8sNodeName == "" || s.kubeletK8sNodeUID == "" {
		return fmt.Errorf("worker Kubernetes node is not captured")
	}
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Kubernetes node %s recreated", s.kubeletK8sNodeName), systemKubeletNodeRecreateTimeout, 30*time.Second, func(waitCtx context.Context) (bool, error) {
		node, found, err := s.k8sNodeByNameOnce(waitCtx, s.kubeletK8sNodeName)
		if err != nil {
			return false, err
		}
		if !found {
			return false, nil
		}
		return string(node.UID) != s.kubeletK8sNodeUID && k8sNodeReady(node), nil
	})
}

func (s *SystemChecks) theSelectedWorkerPodIsRecreatedAndReady(ctx context.Context) error {
	if s.kubeletWorkerPodName == "" || s.kubeletWorkerPodUID == "" {
		return fmt.Errorf("worker pod is not captured")
	}
	return s.runtime.WaitFor(ctx, fmt.Sprintf("worker pod %s recreated and ready", s.kubeletWorkerPodName), systemKubeletWorkerReadyTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		pod, err := s.workerPodByName(waitCtx, s.kubeletWorkerPodName)
		if err != nil {
			return false, err
		}
		return string(pod.UID) != s.kubeletWorkerPodUID && pod.Status.Phase == corev1.PodRunning && kubeobjects.PodReady(pod), nil
	})
}

func (s *SystemChecks) theSelectedSlurmWorkerIsPresentAfterKubeletReplacement(ctx context.Context) error {
	if s.kubeletWorker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Slurm node %s present after kubelet replacement", s.kubeletWorker.Name), systemKubeletSlurmRecoverTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		_, err := s.slurm.NodeInfoOnce(waitCtx, s.kubeletWorker.Name)
		ready := err == nil
		return ready, nil
	})
}

func (s *SystemChecks) theSelectedSlurmWorkerIsUsableAfterKubeletReplacement(ctx context.Context) error {
	if s.kubeletWorker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	if err := s.runtime.WaitFor(ctx, fmt.Sprintf("Slurm node %s usable after kubelet replacement", s.kubeletWorker.Name), systemKubeletSlurmRecoverTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := s.slurm.NodeInfo(waitCtx, s.kubeletWorker.Name)
		if err != nil {
			return false, err
		}
		return node.IsUsable(), nil
	}); err != nil {
		return err
	}
	s.kubeletWorker = framework.WorkerInfo{}
	s.kubeletWorkerPodName = ""
	s.kubeletK8sNodeName = ""
	s.kubeletK8sNodeUID = ""
	s.kubeletWorkerPodUID = ""
	return nil
}

func (s *SystemChecks) theSharedLibslurmSymlinksAreContinuouslyMonitoredFromLogin(ctx context.Context) error {
	if s.workerPod.Name == "" || s.workerPod.UID == "" {
		return fmt.Errorf("worker pod is not selected")
	}

	linkCommand := `python3 -c 'import glob, os, re
libdir = "/usr/lib/" + os.uname().machine + "-linux-gnu"

versioned = [path for path in glob.glob(libdir + "/libslurm.so.*") if re.fullmatch(r"libslurm\.so\.\d+", os.path.basename(path)) and os.path.islink(path)]
links = versioned + [libdir + "/libslurm.so"]
if len(versioned) != 1 or not os.path.islink(links[1]):
    raise SystemExit("expected versioned and unversioned libslurm symlinks, found: " + repr(links))
print(*links, sep="\n")'`
	output, err := s.runtime.Jail().Run(ctx, linkCommand)
	if err != nil {
		return fmt.Errorf("locate libslurm symlinks in login jail: %w", err)
	}
	links := strings.Fields(output)
	if len(links) != 2 {
		return fmt.Errorf("expected two libslurm symlink paths, got %q", strings.TrimSpace(output))
	}

	suffix := string(s.workerPod.UID)
	s.libslurmProbeReady = "/tmp/soperator-acceptance-libslurm-" + suffix + ".ready"
	s.libslurmProbeFailure = "/tmp/soperator-acceptance-libslurm-" + suffix + ".failure"
	s.libslurmProbePID = "/tmp/soperator-acceptance-libslurm-" + suffix + ".pid"
	s.removeLibslurmProbeFiles(ctx)

	probeSource := `import os, sys, time
paths = sys.argv[1:3]
ready_path, failure_path, pid_path = sys.argv[3:]
with open(pid_path, "w", encoding="utf-8") as pid_file:
    pid_file.write(str(os.getpid()))
started = time.monotonic()
iterations = 0
try:
    for path in paths:
        descriptor = os.open(path, os.O_RDONLY)
        os.read(descriptor, 1)
        os.close(descriptor)
    with open(ready_path, "w", encoding="utf-8"):
        pass
    while True:
        for path in paths:
            try:
                descriptor = os.open(path, os.O_RDONLY)
                os.read(descriptor, 1)
                os.close(descriptor)
            except OSError as error:
                with open(failure_path, "w", encoding="utf-8") as failure_file:
                    failure_file.write(f"path={path} iteration={iterations} elapsed={time.monotonic() - started:.6f}s errno={error.errno}: {error}\n")
                    failure_file.flush()
                    os.fsync(failure_file.fileno())
                raise
        iterations += 1
        time.sleep(0.001)
finally:
    try:
        os.unlink(ready_path)
    except FileNotFoundError:
        pass`
	command := fmt.Sprintf(
		"python3 -c %s %s %s %s %s %s",
		framework.ShellQuote(probeSource),
		framework.ShellQuote(links[0]),
		framework.ShellQuote(links[1]),
		framework.ShellQuote(s.libslurmProbeReady),
		framework.ShellQuote(s.libslurmProbeFailure),
		framework.ShellQuote(s.libslurmProbePID),
	)
	probeCtx, cancel := context.WithCancel(ctx)
	results := make(chan libslurmProbeResult, 1)
	s.libslurmProbeCancel = cancel
	s.libslurmProbeResults = results
	go func() {
		probeOutput, probeErr := s.runtime.Jail().Run(probeCtx, command)
		results <- libslurmProbeResult{output: probeOutput, err: probeErr}
	}()

	if err := s.runtime.WaitFor(ctx, "libslurm probe to start", libslurmProbeStartTimeout, 200*time.Millisecond, func(waitCtx context.Context) (bool, error) {
		select {
		case result := <-s.libslurmProbeResults:
			s.libslurmProbeFinished = &result
			return false, libslurmProbeExitError("libslurm probe exited before startup", result)
		default:
		}
		_, markerErr := s.runtime.Jail().Run(waitCtx, fmt.Sprintf("test -f %s", framework.ShellQuote(s.libslurmProbeReady)))
		return markerErr == nil, nil
	}); err != nil {
		s.stopLibslurmProbe(ctx)
		return err
	}

	s.runtime.Logf("system checks: monitoring %s while worker pod %s restarts", strings.Join(links, ", "), s.workerPod.Name)
	return nil
}

func (s *SystemChecks) theSelectedWorkerPodIsRestarted(ctx context.Context) error {
	if s.workerPod.Name == "" || s.workerPod.UID == "" {
		return fmt.Errorf("worker pod is not selected")
	}
	if err := s.ensureLibslurmProbeRunning(); err != nil {
		return err
	}

	s.restartedWorkerPodUID = string(s.workerPod.UID)
	if _, err := s.runtime.Kubectl().Run(ctx,
		"delete", "pod", s.workerPod.Name,
		"-n", framework.SoperatorNamespace,
		"--wait=false",
	); err != nil {
		return fmt.Errorf("restart worker pod %s: %w", s.workerPod.Name, err)
	}
	return nil
}

func (s *SystemChecks) theRestartedWorkerPodIsReady(ctx context.Context) error {
	if s.workerPod.Name == "" || s.restartedWorkerPodUID == "" {
		return fmt.Errorf("worker pod restart was not started")
	}

	waitCtx, cancel := context.WithTimeout(ctx, systemWorkerRestartTimeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastErr error
	for {
		if err := s.ensureLibslurmProbeRunning(); err != nil {
			return err
		}
		pod, found, err := s.workerPodByNameOnce(waitCtx, s.workerPod.Name)
		if err != nil {
			lastErr = err
		} else if found && string(pod.UID) != s.restartedWorkerPodUID && pod.Status.Phase == corev1.PodRunning && kubeobjects.PodReady(pod) {
			return nil
		}

		select {
		case <-waitCtx.Done():
			if lastErr != nil {
				return fmt.Errorf("wait for worker pod %s restarted and ready: %w", s.workerPod.Name, lastErr)
			}
			return fmt.Errorf("wait for worker pod %s restarted and ready: timed out after %s", s.workerPod.Name, systemWorkerRestartTimeout)
		case <-ticker.C:
		}
	}
}

func (s *SystemChecks) theSharedLibslurmSymlinksRemainedContinuouslyReadable(ctx context.Context) error {
	probeErr := s.ensureLibslurmProbeRunning()
	s.stopLibslurmProbe(ctx)

	failure, err := s.runtime.Jail().Run(ctx, fmt.Sprintf("cat %s 2>/dev/null || true", framework.ShellQuote(s.libslurmProbeFailure)))
	if err != nil {
		return fmt.Errorf("read libslurm probe result: %w", err)
	}
	s.removeLibslurmProbeFiles(ctx)
	if failure = strings.TrimSpace(failure); failure != "" {
		return fmt.Errorf("shared libslurm symlink became unavailable: %s", failure)
	}
	if probeErr != nil {
		return probeErr
	}
	return nil
}

func (s *SystemChecks) ensureLibslurmProbeRunning() error {
	if s.libslurmProbeFinished != nil {
		return libslurmProbeExitError("libslurm probe exited unexpectedly", *s.libslurmProbeFinished)
	}
	if s.libslurmProbeResults == nil {
		return fmt.Errorf("libslurm probe is not running")
	}
	select {
	case result := <-s.libslurmProbeResults:
		s.libslurmProbeFinished = &result
		return libslurmProbeExitError("libslurm probe exited unexpectedly", result)
	default:
		return nil
	}
}

func libslurmProbeExitError(message string, result libslurmProbeResult) error {
	output := strings.TrimSpace(result.output)
	if result.err == nil {
		if output == "" {
			return fmt.Errorf("%s without an error", message)
		}
		return fmt.Errorf("%s without an error: %s", message, output)
	}
	if output == "" {
		return fmt.Errorf("%s: %w", message, result.err)
	}
	return fmt.Errorf("%s: %w: %s", message, result.err, output)
}

func (s *SystemChecks) stopLibslurmProbe(ctx context.Context) {
	if s.libslurmProbePID != "" && s.runtime != nil {
		_, _ = s.runtime.Jail().Run(ctx, fmt.Sprintf("test ! -f %[1]s || kill \"$(cat %[1]s)\" >/dev/null 2>&1 || true", framework.ShellQuote(s.libslurmProbePID)))
	}
	if s.libslurmProbeCancel != nil {
		s.libslurmProbeCancel()
	}
	if s.libslurmProbeResults != nil && s.libslurmProbeFinished == nil {
		select {
		case result := <-s.libslurmProbeResults:
			s.libslurmProbeFinished = &result
		case <-time.After(libslurmProbeStopTimeout):
			if s.runtime != nil {
				s.runtime.Logf("cleanup: timed out waiting for libslurm probe to stop")
			}
		}
	}
}

func (s *SystemChecks) removeLibslurmProbeFiles(ctx context.Context) {
	var paths []string
	for _, path := range []string{s.libslurmProbeReady, s.libslurmProbeFailure, s.libslurmProbePID} {
		if path != "" {
			paths = append(paths, framework.ShellQuote(path))
		}
	}
	if len(paths) == 0 || s.runtime == nil {
		return
	}
	if _, err := s.runtime.Jail().Run(ctx, "rm -f "+strings.Join(paths, " ")); err != nil {
		s.runtime.Logf("cleanup: remove libslurm probe files: %v", err)
	}
}

func (s *SystemChecks) resetLibslurmProbe() {
	s.libslurmProbeCancel = nil
	s.libslurmProbeResults = nil
	s.libslurmProbeFinished = nil
	s.libslurmProbeReady = ""
	s.libslurmProbeFailure = ""
	s.libslurmProbePID = ""
}

func (s *SystemChecks) ephemeralInfo(ctx context.Context, pod corev1.Pod) (workerEphemeralInfo, error) {
	limitBytes := podEphemeralLimitBytes(pod)
	if limitBytes == 0 {
		return workerEphemeralInfo{}, fmt.Errorf("pod %s has no ephemeral-storage limit", pod.Name)
	}

	raw, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx, "get", "--raw", fmt.Sprintf("/api/v1/nodes/%s/proxy/stats/summary", pod.Spec.NodeName))
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
	if err := s.kubectl.GetJSON(ctx, &pod, "get", "pod", "-n", framework.SoperatorNamespace, name, "-o", "json"); err != nil {
		return corev1.Pod{}, fmt.Errorf("get worker pod %s: %w", name, err)
	}
	return pod, nil
}

func (s *SystemChecks) workerPodByNameOnce(ctx context.Context, name string) (corev1.Pod, bool, error) {
	output, err := s.runtime.Kubectl().Run(ctx, "get", "pod", "-n", framework.SoperatorNamespace, name, "-o", "json")
	if err != nil {
		if isKubectlNotFound(err) {
			return corev1.Pod{}, false, nil
		}
		return corev1.Pod{}, false, fmt.Errorf("get worker pod %s: %w", name, err)
	}

	var pod corev1.Pod
	if err := json.Unmarshal([]byte(output), &pod); err != nil {
		return corev1.Pod{}, false, fmt.Errorf("decode worker pod %s: %w", name, err)
	}
	return pod, true, nil
}

func (s *SystemChecks) k8sNodeByName(ctx context.Context, name string) (corev1.Node, error) {
	var node corev1.Node
	if err := s.kubectl.GetJSON(ctx, &node, "get", "node", name, "-o", "json"); err != nil {
		return corev1.Node{}, fmt.Errorf("get Kubernetes node %s: %w", name, err)
	}
	return node, nil
}

func (s *SystemChecks) k8sNodeByNameOnce(ctx context.Context, name string) (corev1.Node, bool, error) {
	output, err := s.runtime.Kubectl().Run(ctx, "get", "node", name, "-o", "json")
	if err != nil {
		if isKubectlNotFound(err) {
			return corev1.Node{}, false, nil
		}
		return corev1.Node{}, false, fmt.Errorf("get Kubernetes node %s: %w", name, err)
	}

	var node corev1.Node
	if err := json.Unmarshal([]byte(output), &node); err != nil {
		return corev1.Node{}, false, fmt.Errorf("decode Kubernetes node %s: %w", name, err)
	}
	return node, true, nil
}

func isKubectlNotFound(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "notfound") || strings.Contains(message, "not found")
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
