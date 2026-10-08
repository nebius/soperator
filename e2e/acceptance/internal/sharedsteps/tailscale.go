package sharedsteps

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	tailscaleContainerName         = "tailscale"
	tailscaleAuthenticationPrompt  = "To authenticate, visit:"
	tailscaleAuthenticationTimeout = 2 * time.Minute
	tailscaleStatefulSetResource   = "statefulsets.apps.kruise.io"
	tailscaleInstanceLabel         = "app.kubernetes.io/instance"
	tailscaleComponentLabel        = "app.kubernetes.io/component"
	tailscaleLoginComponent        = "login"
)

type Tailscale struct {
	info       *framework.ClusterInfo
	runtime    framework.Runtime
	kubectl    *framework.KubectlClient
	configured bool
}

func NewTailscale(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	kubectl *framework.KubectlClient,
) *Tailscale {
	return &Tailscale{
		info:    info,
		runtime: runtime,
		kubectl: kubectl,
	}
}

func (s *Tailscale) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^Tailscale is configured for the login workload$`, s.tailscaleIsConfigured)
	sc.Step(`^every login pod reports a Tailscale authentication URL$`, s.everyLoginPodReportsAuthenticationURL)
}

func (s *Tailscale) CleanupAndReset(context.Context) {
	s.configured = false
}

func (s *Tailscale) tailscaleIsConfigured(ctx context.Context) error {
	var cluster kubeobjects.SlurmCluster
	if err := s.kubectl.GetJSON(ctx, &cluster,
		"get", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "-o", "json",
	); err != nil {
		return fmt.Errorf("get SlurmCluster %s/%s: %w", framework.SoperatorNamespace, s.info.SlurmClusterName, err)
	}

	if cluster.Spec.SlurmNodes.Login.Size < 1 {
		s.runtime.Logf("Acceptance: login replicas are disabled, skipping Tailscale scenario")
		return godog.ErrSkip
	}
	if !hasNamedContainer(cluster.Spec.SlurmNodes.Login.CustomInitContainers, tailscaleContainerName) {
		s.runtime.Logf("Acceptance: Tailscale is not configured for login pods, skipping scenario")
		return godog.ErrSkip
	}

	s.configured = true
	return nil
}

func (s *Tailscale) everyLoginPodReportsAuthenticationURL(ctx context.Context) error {
	if !s.configured {
		return fmt.Errorf("check Tailscale authentication URLs: Tailscale configuration was not verified")
	}

	var podNames []string
	err := s.runtime.WaitFor(
		ctx,
		"every login pod to report a Tailscale authentication URL",
		tailscaleAuthenticationTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			statefulSet, err := s.loginStatefulSet(waitCtx)
			if err != nil {
				return false, err
			}
			pods, err := s.loginPods(waitCtx)
			if err != nil {
				return false, err
			}

			podNames, err = validateTailscaleLoginPods(statefulSet, pods)
			if err != nil {
				return false, err
			}
			for _, podName := range podNames {
				logs, err := s.runtime.Kubectl().Run(waitCtx,
					"logs", "-n", framework.SoperatorNamespace, podName,
					"-c", tailscaleContainerName,
				)
				if err != nil {
					return false, fmt.Errorf("read Tailscale logs for login pod %s: %w", podName, err)
				}
				if !tailscaleAuthenticationURLPresent(logs) {
					return false, fmt.Errorf("login pod %s does not report a Tailscale authentication URL", podName)
				}
			}
			return true, nil
		},
	)
	if err != nil {
		return err
	}

	s.runtime.Logf("Tailscale: login pods %s report authentication URLs", strings.Join(podNames, ", "))
	return nil
}

func (s *Tailscale) loginStatefulSet(ctx context.Context) (kubeobjects.LoginStatefulSet, error) {
	var statefulSets kubeobjects.LoginStatefulSetList
	if err := s.kubectl.GetJSON(ctx, &statefulSets,
		"get", tailscaleStatefulSetResource,
		"-n", framework.SoperatorNamespace,
		"-l", s.loginSelector(), "-o", "json",
	); err != nil {
		return kubeobjects.LoginStatefulSet{}, fmt.Errorf("list login StatefulSets: %w", err)
	}
	if len(statefulSets.Items) != 1 {
		return kubeobjects.LoginStatefulSet{}, fmt.Errorf("find login StatefulSet: got %d, expected 1", len(statefulSets.Items))
	}
	return statefulSets.Items[0], nil
}

func (s *Tailscale) loginPods(ctx context.Context) ([]corev1.Pod, error) {
	var pods corev1.PodList
	if err := s.kubectl.GetJSON(ctx, &pods,
		"get", "pods", "-n", framework.SoperatorNamespace,
		"-l", s.loginSelector(), "-o", "json",
	); err != nil {
		return nil, fmt.Errorf("list login pods: %w", err)
	}
	return pods.Items, nil
}

func (s *Tailscale) loginSelector() string {
	return fmt.Sprintf("%s=%s,%s=%s",
		tailscaleInstanceLabel, s.info.SlurmClusterName,
		tailscaleComponentLabel, tailscaleLoginComponent,
	)
}

func validateTailscaleLoginPods(statefulSet kubeobjects.LoginStatefulSet, pods []corev1.Pod) ([]string, error) {
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas < 1 {
		return nil, fmt.Errorf("validate login StatefulSet replicas: got %v, expected at least 1", replicaValue(statefulSet.Spec.Replicas))
	}
	expected := *statefulSet.Spec.Replicas
	if statefulSet.Status.ReadyReplicas != expected {
		return nil, fmt.Errorf("validate login StatefulSet readiness: got %d ready replicas, expected %d", statefulSet.Status.ReadyReplicas, expected)
	}

	activePods := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if pod.DeletionTimestamp == nil {
			activePods = append(activePods, pod)
		}
	}
	if len(activePods) != int(expected) {
		return nil, fmt.Errorf("validate active login pods: got %d, expected %d", len(activePods), expected)
	}

	podNames := make([]string, 0, len(activePods))
	for _, pod := range activePods {
		if pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(pod) {
			return nil, fmt.Errorf("validate login pod %s: phase=%s ready=%t", pod.Name, pod.Status.Phase, kubeobjects.PodReady(pod))
		}
		if !hasCoreContainer(pod.Spec.InitContainers, tailscaleContainerName) {
			return nil, fmt.Errorf("validate login pod %s: init container %s is missing", pod.Name, tailscaleContainerName)
		}
		podNames = append(podNames, pod.Name)
	}
	sort.Strings(podNames)
	return podNames, nil
}

func hasNamedContainer(containers []kubeobjects.NamedContainer, name string) bool {
	for _, container := range containers {
		if container.Name == name {
			return true
		}
	}
	return false
}

func hasCoreContainer(containers []corev1.Container, name string) bool {
	for _, container := range containers {
		if container.Name == name {
			return true
		}
	}
	return false
}

func tailscaleAuthenticationURLPresent(logs string) bool {
	promptIndex := strings.Index(logs, tailscaleAuthenticationPrompt)
	if promptIndex < 0 {
		return false
	}

	for _, field := range strings.Fields(logs[promptIndex+len(tailscaleAuthenticationPrompt):]) {
		candidate := strings.Trim(field, `"'()[]{}<>,;`)
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "login.tailscale.com" {
			continue
		}
		if token := strings.TrimPrefix(parsed.Path, "/a/"); token != parsed.Path && token != "" {
			return true
		}
	}
	return false
}
