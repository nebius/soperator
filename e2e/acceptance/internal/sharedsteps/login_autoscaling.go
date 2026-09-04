package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/cucumber/godog"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	loginAutoscalingTargetCPU            = int32(10)
	loginAutoscalingTimeout              = 10 * time.Minute
	loginAutoscalingCleanupTimeout       = 5 * time.Minute
	loginAutoscalingObservationPeriod    = 15 * time.Second
	loginAutoscalingObservationPoll      = 3 * time.Second
	loginAutoscalingPressurePercent      = int64(30)
	loginAutoscalingMaxPressureProcesses = int32(16)
	loginAutoscalingPressurePIDFile      = "/tmp/soperator-acceptance-login-autoscaling.pids"
	loginAutoscalingStatefulSetResource  = "statefulsets.apps.kruise.io"
	loginAutoscalingHPAResource          = "horizontalpodautoscalers.autoscaling"
	loginAutoscalingSSHDContainer        = "sshd"
	loginAutoscalingInstanceLabel        = "app.kubernetes.io/instance"
	loginAutoscalingComponentLabel       = "app.kubernetes.io/component"
	loginAutoscalingComponentLabelValue  = "login"
)

type LoginAutoscaling struct {
	info    *framework.ClusterInfo
	runtime framework.Runtime
	kubectl *framework.KubectlClient

	initialConfigCaptured bool
	initialSize           int32
	initialAutoscaling    json.RawMessage
	statefulSetName       string
	maxReplicas           int32
	initialK8sNodes       map[string]string
	pressurePods          []string
	scaledPods            map[string]string
}

func NewLoginAutoscaling(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	kubectl *framework.KubectlClient,
) *LoginAutoscaling {
	return &LoginAutoscaling{
		info:    info,
		runtime: runtime,
		kubectl: kubectl,
	}
}

func (s *LoginAutoscaling) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the login workload is ready for an autoscaling lifecycle test$`, s.selectReadyLoginWorkload)
	sc.Step(`^login autoscaling is enabled with one additional replica of capacity$`, s.enableLoginAutoscaling)
	sc.Step(`^the login workload remains at its default replica count without pressure$`, s.checkLoginDoesNotScaleWithoutPressure)
	sc.Step(`^CPU pressure is created on the login pods$`, s.createLoginCPUPressure)
	sc.Step(`^the login workload scales to its autoscaling maximum$`, s.waitForLoginScaleUp)
	sc.Step(`^CPU pressure is removed from the login pods$`, s.removeLoginCPUPressure)
	sc.Step(`^the login workload does not scale down automatically$`, s.checkLoginDoesNotScaleDown)
	sc.Step(`^the fixed login size is changed while autoscaling is enabled$`, s.changeFixedLoginSize)
	sc.Step(`^the autoscaled login pods are preserved without recreation$`, s.checkAutoscaledLoginPodsPreserved)
	sc.Step(`^login autoscaling is disabled$`, s.disableLoginAutoscaling)
	sc.Step(`^the login workload scales down to the fixed size$`, s.waitForLoginScaleDown)
}

func (s *LoginAutoscaling) CleanupAndReset(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, loginAutoscalingCleanupTimeout)
	defer cancel()

	if err := s.removeLoginCPUPressure(cleanupCtx); err != nil {
		s.runtime.Logf("cleanup: remove login CPU pressure: %v", err)
	}
	if s.initialConfigCaptured {
		if err := s.restoreInitialLoginConfig(cleanupCtx); err != nil {
			s.runtime.Logf("cleanup: restore initial login configuration: %v", err)
		} else if err := s.waitForInitialLoginConfig(cleanupCtx); err != nil {
			s.runtime.Logf("cleanup: wait for initial login configuration: %v", err)
		}
	}

	s.initialConfigCaptured = false
	s.initialSize = 0
	s.initialAutoscaling = nil
	s.statefulSetName = ""
	s.maxReplicas = 0
	s.initialK8sNodes = nil
	s.pressurePods = nil
	s.scaledPods = nil
}

func (s *LoginAutoscaling) selectReadyLoginWorkload(ctx context.Context) error {
	var cluster kubeobjects.LoginAutoscalingCluster
	if err := s.kubectl.GetJSON(ctx, &cluster,
		"get", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "-o", "json",
	); err != nil {
		return fmt.Errorf("get SlurmCluster: %w", err)
	}
	s.initialSize = cluster.Spec.SlurmNodes.Login.Size
	if s.initialSize < 1 {
		s.runtime.Logf("acceptance: login nodes are disabled, skipping login autoscaling scenario")
		return godog.ErrSkip
	}
	s.initialConfigCaptured = true
	s.initialAutoscaling = append(json.RawMessage(nil), cluster.Spec.SlurmNodes.Login.Autoscaling...)
	initialAutoscalingEnabled, err := loginAutoscalingEnabled(s.initialAutoscaling)
	if err != nil {
		return err
	}
	if initialAutoscalingEnabled {
		if err := s.patchLogin(ctx, map[string]any{
			"autoscaling": map[string]any{"enabled": false},
		}); err != nil {
			return err
		}
		s.runtime.Logf("login autoscaling: disabled the initial HPA for the test baseline")
	}

	if err := s.runtime.WaitFor(ctx, fmt.Sprintf("ready login StatefulSet with its default %d replicas", s.initialSize),
		loginAutoscalingTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			statefulSet, err := s.loginStatefulSet(waitCtx)
			if err != nil {
				return false, err
			}
			pods, err := s.loginPods(waitCtx)
			if err != nil {
				return false, err
			}
			if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != s.initialSize ||
				statefulSet.Status.ReadyReplicas != s.initialSize || int32(len(pods)) != s.initialSize {
				return false, fmt.Errorf("StatefulSet spec=%v ready=%d, login pods=%d, expected default size %d",
					replicaValue(statefulSet.Spec.Replicas), statefulSet.Status.ReadyReplicas, len(pods), s.initialSize)
			}
			for _, pod := range pods {
				if pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(pod) {
					return false, fmt.Errorf("login pod %s phase=%s ready=%t", pod.Name, pod.Status.Phase, kubeobjects.PodReady(pod))
				}
			}
			s.statefulSetName = statefulSet.Metadata.Name
			return true, nil
		},
	); err != nil {
		return err
	}
	if err := s.waitForLoginHPARemoval(ctx); err != nil {
		return err
	}
	nodes, err := s.k8sNodes(ctx)
	if err != nil {
		return err
	}
	s.initialK8sNodes = snapshotNodes(nodes)

	s.maxReplicas = s.initialSize + 1
	s.runtime.Logf("login autoscaling: selected StatefulSet=%s min=%d max=%d",
		s.statefulSetName, s.initialSize, s.maxReplicas)
	return nil
}

func (s *LoginAutoscaling) checkLoginDoesNotScaleWithoutPressure(ctx context.Context) error {
	if err := s.waitForReadyLoginReplicas(ctx, s.initialSize); err != nil {
		return err
	}
	pods, err := s.loginPods(ctx)
	if err != nil {
		return err
	}
	return s.observePodSnapshot(ctx, snapshotPods(pods), loginAutoscalingObservationPeriod)
}

func (s *LoginAutoscaling) enableLoginAutoscaling(ctx context.Context) error {
	if s.statefulSetName == "" {
		return fmt.Errorf("login workload is not selected")
	}
	// This intentionally differs from both the live and maximum replica counts. Autoscaling must ignore it until disabled.
	testFixedSize := s.maxReplicas + 1
	if err := s.patchLogin(ctx, map[string]any{
		"size": testFixedSize,
		"autoscaling": map[string]any{
			"enabled":                        true,
			"minReplicas":                    s.initialSize,
			"maxReplicas":                    s.maxReplicas,
			"targetCPUUtilizationPercentage": loginAutoscalingTargetCPU,
		},
	}); err != nil {
		return err
	}

	return s.runtime.WaitFor(ctx, "login HPA with configured replica bounds",
		loginAutoscalingTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			hpa, exists, err := s.loginHPA(waitCtx)
			if err != nil {
				return false, err
			}
			if !exists {
				return false, fmt.Errorf("login HPA %s does not exist", s.statefulSetName)
			}
			if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != s.initialSize ||
				hpa.Spec.MaxReplicas != s.maxReplicas {
				return false, fmt.Errorf("login HPA bounds min=%v max=%d, expected min=%d max=%d",
					replicaValue(hpa.Spec.MinReplicas), hpa.Spec.MaxReplicas, s.initialSize, s.maxReplicas)
			}
			return true, nil
		},
	)
}

func (s *LoginAutoscaling) createLoginCPUPressure(ctx context.Context) error {
	pods, err := s.loginPods(ctx)
	if err != nil {
		return err
	}
	if int32(len(pods)) != s.initialSize {
		return fmt.Errorf("found %d login pods before pressure, expected %d", len(pods), s.initialSize)
	}

	for _, pod := range pods {
		processes, err := loginPressureProcessCount(pod)
		if err != nil {
			return err
		}
		script := fmt.Sprintf(
			`pid_file=%s; if [ -f "$pid_file" ]; then for pid in $(cat "$pid_file"); do kill "$pid" 2>/dev/null || true; done; fi; : > "$pid_file"; i=0; while [ "$i" -lt %d ]; do sh -c 'trap "" HUP; while :; do :; done' >/dev/null 2>&1 & echo "$!" >> "$pid_file"; i=$((i + 1)); done`,
			framework.ShellQuote(loginAutoscalingPressurePIDFile),
			processes,
		)
		// Track the pod before exec so scenario cleanup retries removal even if exec reports a partial failure.
		s.pressurePods = append(s.pressurePods, pod.Name)
		if _, err := s.runtime.Kubectl().Run(ctx,
			"exec", "-n", framework.SoperatorNamespace, pod.Name,
			"-c", loginAutoscalingSSHDContainer, "--", "sh", "-c", script,
		); err != nil {
			return fmt.Errorf("create CPU pressure in login pod %s: %w", pod.Name, err)
		}
		s.runtime.Logf("login autoscaling: started %d CPU pressure processes in pod %s", processes, pod.Name)
	}
	return nil
}

func (s *LoginAutoscaling) waitForLoginScaleUp(ctx context.Context) error {
	if err := s.waitForReadyLoginReplicas(ctx, s.maxReplicas); err != nil {
		return err
	}
	if err := s.runtime.WaitFor(ctx, "login HPA to report maximum desired replicas",
		loginAutoscalingTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			hpa, exists, err := s.loginHPA(waitCtx)
			if err != nil {
				return false, err
			}
			if !exists {
				return false, fmt.Errorf("login HPA %s does not exist", s.statefulSetName)
			}
			if hpa.Status.DesiredReplicas != s.maxReplicas || hpa.Status.CurrentReplicas != s.maxReplicas {
				return false, fmt.Errorf("login HPA current=%d desired=%d, expected %d",
					hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas, s.maxReplicas)
			}
			return true, nil
		},
	); err != nil {
		return err
	}
	pods, err := s.loginPods(ctx)
	if err != nil {
		return err
	}
	nodes, err := s.k8sNodes(ctx)
	if err != nil {
		return err
	}
	newNode, err := loginPodOnNewNode(pods, nodes, s.initialK8sNodes)
	if err != nil {
		return err
	}
	if newNode == "" {
		return fmt.Errorf("all scaled login pods are running on Kubernetes nodes that existed before pressure; node-group scale-up was not observed")
	}
	s.runtime.Logf("login autoscaling: Kubernetes node-group scale-up added node %s", newNode)
	s.scaledPods = snapshotPods(pods)
	return nil
}

func (s *LoginAutoscaling) removeLoginCPUPressure(ctx context.Context) error {
	var problems []string
	var remainingPods []string
	for _, podName := range s.pressurePods {
		script := fmt.Sprintf(
			`pid_file=%s; if [ -f "$pid_file" ]; then for pid in $(cat "$pid_file"); do kill "$pid" 2>/dev/null || true; done; rm -f "$pid_file"; fi`,
			framework.ShellQuote(loginAutoscalingPressurePIDFile),
		)
		if _, err := s.runtime.Kubectl().Run(ctx,
			"exec", "-n", framework.SoperatorNamespace, podName,
			"-c", loginAutoscalingSSHDContainer, "--", "sh", "-c", script,
		); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", podName, err))
			remainingPods = append(remainingPods, podName)
		}
	}
	s.pressurePods = remainingPods
	if len(problems) > 0 {
		return fmt.Errorf("remove CPU pressure from login pods: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *LoginAutoscaling) checkLoginDoesNotScaleDown(ctx context.Context) error {
	if err := s.runtime.WaitFor(ctx, "login CPU utilization below the HPA target",
		loginAutoscalingTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			hpa, exists, err := s.loginHPA(waitCtx)
			if err != nil {
				return false, err
			}
			if !exists {
				return false, fmt.Errorf("login HPA %s does not exist", s.statefulSetName)
			}
			for _, metric := range hpa.Status.CurrentMetrics {
				if metric.Type != autoscalingv2.ContainerResourceMetricSourceType || metric.ContainerResource == nil ||
					metric.ContainerResource.Container != loginAutoscalingSSHDContainer ||
					metric.ContainerResource.Name != corev1.ResourceCPU {
					continue
				}
				utilization := metric.ContainerResource.Current.AverageUtilization
				if utilization == nil {
					return false, fmt.Errorf("login HPA has no current CPU utilization")
				}
				if *utilization >= loginAutoscalingTargetCPU {
					return false, fmt.Errorf("login HPA CPU utilization=%d, target=%d",
						*utilization, loginAutoscalingTargetCPU)
				}
				return true, nil
			}
			return false, fmt.Errorf("login HPA has no SSHD container CPU metric")
		},
	); err != nil {
		return err
	}
	return s.observePodSnapshot(ctx, s.scaledPods, loginAutoscalingObservationPeriod)
}

func (s *LoginAutoscaling) changeFixedLoginSize(ctx context.Context) error {
	return s.patchLogin(ctx, map[string]any{"size": s.initialSize})
}

func (s *LoginAutoscaling) checkAutoscaledLoginPodsPreserved(ctx context.Context) error {
	return s.observePodSnapshot(ctx, s.scaledPods, loginAutoscalingObservationPeriod)
}

func (s *LoginAutoscaling) disableLoginAutoscaling(ctx context.Context) error {
	return s.patchLogin(ctx, map[string]any{
		"autoscaling": map[string]any{"enabled": false},
	})
}

func (s *LoginAutoscaling) waitForLoginScaleDown(ctx context.Context) error {
	if err := s.waitForLoginHPARemoval(ctx); err != nil {
		return err
	}
	return s.waitForReadyLoginReplicas(ctx, s.initialSize)
}

func (s *LoginAutoscaling) waitForLoginHPARemoval(ctx context.Context) error {
	return s.runtime.WaitFor(ctx, "login HPA removal",
		loginAutoscalingTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			_, exists, err := s.loginHPA(waitCtx)
			return !exists, err
		},
	)
}

func (s *LoginAutoscaling) waitForReadyLoginReplicas(ctx context.Context, expected int32) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("%d ready login replicas", expected),
		loginAutoscalingTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			statefulSet, err := s.loginStatefulSet(waitCtx)
			if err != nil {
				return false, err
			}
			pods, err := s.loginPods(waitCtx)
			if err != nil {
				return false, err
			}
			if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != expected ||
				statefulSet.Status.ReadyReplicas != expected || int32(len(pods)) != expected {
				return false, fmt.Errorf("StatefulSet spec=%v ready=%d pods=%d, expected %d",
					replicaValue(statefulSet.Spec.Replicas), statefulSet.Status.ReadyReplicas, len(pods), expected)
			}
			for _, pod := range pods {
				if pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(pod) {
					return false, fmt.Errorf("login pod %s phase=%s ready=%t", pod.Name, pod.Status.Phase, kubeobjects.PodReady(pod))
				}
			}
			return true, nil
		},
	)
}

func (s *LoginAutoscaling) observePodSnapshot(ctx context.Context, expected map[string]string, duration time.Duration) error {
	if len(expected) == 0 {
		return fmt.Errorf("login pod snapshot is empty")
	}
	observationTimer := time.NewTimer(duration)
	defer observationTimer.Stop()
	ticker := time.NewTicker(loginAutoscalingObservationPoll)
	defer ticker.Stop()

	for {
		// Use the scenario context for the probe. The observation timer marks a
		// successful, unchanged interval; it must not cancel a final probe at the
		// timer boundary and turn successful observation into a test failure.
		pods, err := s.loginPods(ctx)
		if err != nil {
			return err
		}
		actual := snapshotPods(pods)
		if !maps.Equal(actual, expected) {
			return fmt.Errorf("login pods changed: got %v, expected %v", actual, expected)
		}
		for _, pod := range pods {
			if pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(pod) {
				return fmt.Errorf("login pod %s is not Running and Ready", pod.Name)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-observationTimer.C:
			return nil
		case <-ticker.C:
		}
	}
}

func (s *LoginAutoscaling) loginStatefulSet(ctx context.Context) (kubeobjects.LoginStatefulSet, error) {
	var statefulSets kubeobjects.LoginStatefulSetList
	if err := s.kubectl.GetJSON(ctx, &statefulSets,
		"get", loginAutoscalingStatefulSetResource,
		"-n", framework.SoperatorNamespace,
		"-l", s.loginSelector(), "-o", "json",
	); err != nil {
		return kubeobjects.LoginStatefulSet{}, fmt.Errorf("list login StatefulSets: %w", err)
	}
	if len(statefulSets.Items) != 1 {
		return kubeobjects.LoginStatefulSet{}, fmt.Errorf("found %d login StatefulSets, expected 1", len(statefulSets.Items))
	}
	return statefulSets.Items[0], nil
}

func (s *LoginAutoscaling) loginPods(ctx context.Context) ([]corev1.Pod, error) {
	var pods corev1.PodList
	if err := s.kubectl.GetJSON(ctx, &pods,
		"get", "pods", "-n", framework.SoperatorNamespace,
		"-l", s.loginSelector(), "-o", "json",
	); err != nil {
		return nil, fmt.Errorf("list login pods: %w", err)
	}
	return pods.Items, nil
}

func (s *LoginAutoscaling) k8sNodes(ctx context.Context) ([]corev1.Node, error) {
	var nodes corev1.NodeList
	if err := s.kubectl.GetJSON(ctx, &nodes, "get", "nodes", "-o", "json"); err != nil {
		return nil, fmt.Errorf("list Kubernetes nodes: %w", err)
	}
	return nodes.Items, nil
}

func (s *LoginAutoscaling) loginHPA(ctx context.Context) (autoscalingv2.HorizontalPodAutoscaler, bool, error) {
	var hpa autoscalingv2.HorizontalPodAutoscaler
	output, err := s.runtime.Kubectl().Run(ctx,
		"get", loginAutoscalingHPAResource, s.statefulSetName,
		"-n", framework.SoperatorNamespace, "--ignore-not-found", "-o", "json",
	)
	if err != nil {
		return hpa, false, fmt.Errorf("get login HPA: %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return hpa, false, nil
	}
	if err := json.Unmarshal([]byte(output), &hpa); err != nil {
		return hpa, false, fmt.Errorf("decode login HPA: %w", err)
	}
	return hpa, true, nil
}

func (s *LoginAutoscaling) loginSelector() string {
	return fmt.Sprintf("%s=%s,%s=%s",
		loginAutoscalingInstanceLabel, s.info.SlurmClusterName,
		loginAutoscalingComponentLabel, loginAutoscalingComponentLabelValue,
	)
}

func (s *LoginAutoscaling) patchLogin(ctx context.Context, loginPatch map[string]any) error {
	patch := map[string]any{
		"spec": map[string]any{
			"slurmNodes": map[string]any{
				"login": loginPatch,
			},
		},
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal login patch: %w", err)
	}
	if _, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
		"patch", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "--type=merge", "-p", string(data),
	); err != nil {
		return fmt.Errorf("patch login configuration: %w", err)
	}
	return nil
}

func (s *LoginAutoscaling) restoreInitialLoginConfig(ctx context.Context) error {
	if err := s.patchLogin(ctx, map[string]any{
		"size":        s.initialSize,
		"autoscaling": nil,
	}); err != nil {
		return err
	}
	if len(s.initialAutoscaling) == 0 || string(s.initialAutoscaling) == "null" {
		return nil
	}
	var autoscaling any
	if err := json.Unmarshal(s.initialAutoscaling, &autoscaling); err != nil {
		return fmt.Errorf("decode initial login autoscaling configuration: %w", err)
	}
	return s.patchLogin(ctx, map[string]any{"autoscaling": autoscaling})
}

func (s *LoginAutoscaling) waitForInitialLoginConfig(ctx context.Context) error {
	enabled, err := loginAutoscalingEnabled(s.initialAutoscaling)
	if err != nil {
		return err
	}
	if enabled {
		return s.runtime.WaitFor(ctx, "restored login HPA",
			loginAutoscalingCleanupTimeout, framework.DefaultPollInterval,
			func(waitCtx context.Context) (bool, error) {
				_, exists, err := s.loginHPA(waitCtx)
				return exists, err
			},
		)
	}
	if err := s.runtime.WaitFor(ctx, "login HPA cleanup",
		loginAutoscalingCleanupTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			_, exists, err := s.loginHPA(waitCtx)
			return !exists, err
		},
	); err != nil {
		return err
	}
	return s.waitForReadyLoginReplicas(ctx, s.initialSize)
}

func loginAutoscalingEnabled(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	var autoscaling struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &autoscaling); err != nil {
		return false, fmt.Errorf("decode initial login autoscaling state: %w", err)
	}
	return autoscaling.Enabled, nil
}

func loginPressureProcessCount(pod corev1.Pod) (int32, error) {
	for _, container := range pod.Spec.Containers {
		if container.Name != loginAutoscalingSSHDContainer {
			continue
		}
		cpuRequest := container.Resources.Requests.Cpu().MilliValue()
		if cpuRequest < 1 {
			return 0, fmt.Errorf("login pod %s has no positive SSHD CPU request", pod.Name)
		}
		processes := int32((cpuRequest*loginAutoscalingPressurePercent + 100_000 - 1) / 100_000)
		if processes < 1 {
			processes = 1
		}
		if processes > loginAutoscalingMaxPressureProcesses {
			return 0, fmt.Errorf(
				"login pod %s requires %d pressure processes; refusing to exceed the safety limit of %d",
				pod.Name, processes, loginAutoscalingMaxPressureProcesses,
			)
		}
		return processes, nil
	}
	return 0, fmt.Errorf("login pod %s has no %s container", pod.Name, loginAutoscalingSSHDContainer)
}

func snapshotPods(pods []corev1.Pod) map[string]string {
	snapshot := make(map[string]string, len(pods))
	for _, pod := range pods {
		snapshot[pod.Name] = string(pod.UID)
	}
	return snapshot
}

func replicaValue(replicas *int32) any {
	if replicas == nil {
		return "<nil>"
	}
	return *replicas
}

func snapshotNodes(nodes []corev1.Node) map[string]string {
	snapshot := make(map[string]string, len(nodes))
	for _, node := range nodes {
		snapshot[node.Name] = string(node.UID)
	}
	return snapshot
}

func loginPodOnNewNode(pods []corev1.Pod, nodes []corev1.Node, initialNodes map[string]string) (string, error) {
	currentNodes := snapshotNodes(nodes)
	usedNodes := make(map[string]struct{}, len(pods))
	newNode := ""
	for _, pod := range pods {
		if pod.Spec.NodeName == "" {
			return "", fmt.Errorf("login pod %s is not assigned to a Kubernetes node", pod.Name)
		}
		if _, duplicate := usedNodes[pod.Spec.NodeName]; duplicate {
			return "", fmt.Errorf("multiple login pods are assigned to Kubernetes node %s", pod.Spec.NodeName)
		}
		usedNodes[pod.Spec.NodeName] = struct{}{}

		currentUID, exists := currentNodes[pod.Spec.NodeName]
		if !exists {
			return "", fmt.Errorf("Kubernetes node %s used by login pod %s was not returned by the API", pod.Spec.NodeName, pod.Name)
		}
		if initialUID, existedInitially := initialNodes[pod.Spec.NodeName]; !existedInitially || initialUID != currentUID {
			newNode = pod.Spec.NodeName
		}
	}
	return newNode, nil
}
