package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	tailscaleContainerName  = "tailscale"
	tailscaleStatusTimeout  = 3 * time.Minute
	tailscaleRestartTimeout = 10 * time.Minute
)

type Tailscale struct {
	info    *framework.ClusterInfo
	runtime framework.Runtime
	kubectl *framework.KubectlClient

	loginPods       []corev1.Pod
	stateSecretUIDs map[string]types.UID
	restartedPod    corev1.Pod
	restartedPodUID types.UID
}

type tailscaleStatus struct {
	TUN          bool   `json:"TUN"`
	BackendState string `json:"BackendState"`
	AuthURL      string `json:"AuthURL"`
}

func NewTailscale(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	kubectl *framework.KubectlClient,
) *Tailscale {
	return &Tailscale{info: info, runtime: runtime, kubectl: kubectl}
}

func (s *Tailscale) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^Tailscale is enabled on login pods$`, s.tailscaleIsEnabledOnLoginPods)
	sc.Step(`^the Tailscale login pods are discovered from the live cluster$`, s.discoverTailscaleLoginPods)
	sc.Step(`^every Tailscale sidecar is waiting for authentication$`, s.allTailscaleSidecarsWaitForAuthentication)
	sc.Step(`^every login pod has a persisted Tailscale state Secret$`, s.allLoginPodsHaveStateSecrets)
	sc.Step(`^one Tailscale login pod is restarted$`, s.restartOneTailscaleLoginPod)
	sc.Step(`^the restarted Tailscale sidecar is waiting for authentication$`, s.restartedTailscaleSidecarWaitsForAuthentication)
	sc.Step(`^the restarted login pod reuses its Tailscale state Secret$`, s.restartedLoginPodReusesStateSecret)
}

func (s *Tailscale) CleanupAndReset(ctx context.Context) {
	s.loginPods = nil
	s.stateSecretUIDs = nil
	s.restartedPod = corev1.Pod{}
	s.restartedPodUID = ""
}

func (s *Tailscale) tailscaleIsEnabledOnLoginPods(ctx context.Context) error {
	cluster, err := s.kubectl.SlurmCluster(ctx, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	if !cluster.LoginTailscaleEnabled {
		s.runtime.Logf("Tailscale is not configured on login pods; skipping scenario")
		return godog.ErrSkip
	}
	return nil
}

func (s *Tailscale) discoverTailscaleLoginPods(ctx context.Context) error {
	selector := fmt.Sprintf(
		"app.kubernetes.io/instance=%s,app.kubernetes.io/component=login",
		s.info.SlurmClusterName,
	)
	var pods corev1.PodList
	if err := s.kubectl.GetJSON(ctx, &pods,
		"get", "pods", "-n", framework.SoperatorNamespace,
		"-l", selector, "-o", "json",
	); err != nil {
		return fmt.Errorf("list Tailscale login pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no login pods found for SlurmCluster %s", s.info.SlurmClusterName)
	}

	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].Name < pods.Items[j].Name
	})
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(pod) {
			return fmt.Errorf("login pod %s phase=%s ready=%t", pod.Name, pod.Status.Phase, kubeobjects.PodReady(pod))
		}
		if err := validateTailscaleSidecar(pod); err != nil {
			return err
		}
	}

	s.loginPods = pods.Items
	return nil
}

func (s *Tailscale) allTailscaleSidecarsWaitForAuthentication(ctx context.Context) error {
	if len(s.loginPods) == 0 {
		return fmt.Errorf("tailscale login pods were not discovered")
	}
	for _, pod := range s.loginPods {
		if err := s.waitForUnauthenticatedStatus(ctx, pod.Name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Tailscale) allLoginPodsHaveStateSecrets(ctx context.Context) error {
	if len(s.loginPods) == 0 {
		return fmt.Errorf("tailscale login pods were not discovered")
	}

	s.stateSecretUIDs = make(map[string]types.UID, len(s.loginPods))
	for _, pod := range s.loginPods {
		secret, err := s.waitForStateSecret(ctx, pod.Name)
		if err != nil {
			return err
		}
		s.stateSecretUIDs[pod.Name] = secret.UID
	}
	return nil
}

func (s *Tailscale) restartOneTailscaleLoginPod(ctx context.Context) error {
	if len(s.loginPods) == 0 || len(s.stateSecretUIDs) == 0 {
		return fmt.Errorf("tailscale login pods and state Secrets were not inspected")
	}

	pod := s.loginPods[0]
	s.restartedPodUID = pod.UID
	if _, err := s.runtime.Kubectl().Run(ctx,
		"delete", "pod", pod.Name,
		"-n", framework.SoperatorNamespace,
		"--wait=false",
	); err != nil {
		return fmt.Errorf("restart Tailscale login pod %s: %w", pod.Name, err)
	}

	return s.runtime.WaitFor(ctx, fmt.Sprintf("Tailscale login pod %s restarted and ready", pod.Name),
		tailscaleRestartTimeout, 2*time.Second,
		func(waitCtx context.Context) (bool, error) {
			var current corev1.Pod
			if err := s.kubectl.GetJSON(waitCtx, &current,
				"get", "pod", pod.Name, "-n", framework.SoperatorNamespace, "-o", "json",
			); err != nil {
				return false, err
			}
			if current.UID == s.restartedPodUID {
				return false, fmt.Errorf("login pod %s still has UID %s", pod.Name, current.UID)
			}
			if current.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(current) {
				return false, fmt.Errorf("login pod %s phase=%s ready=%t", current.Name, current.Status.Phase, kubeobjects.PodReady(current))
			}
			if err := validateTailscaleSidecar(current); err != nil {
				return false, err
			}
			s.restartedPod = current
			return true, nil
		},
	)
}

func (s *Tailscale) restartedTailscaleSidecarWaitsForAuthentication(ctx context.Context) error {
	if s.restartedPod.Name == "" {
		return fmt.Errorf("tailscale login pod was not restarted")
	}
	return s.waitForUnauthenticatedStatus(ctx, s.restartedPod.Name)
}

func (s *Tailscale) restartedLoginPodReusesStateSecret(ctx context.Context) error {
	if s.restartedPod.Name == "" {
		return fmt.Errorf("tailscale login pod was not restarted")
	}
	expectedUID, ok := s.stateSecretUIDs[s.restartedPod.Name]
	if !ok {
		return fmt.Errorf("state Secret UID for login pod %s was not recorded", s.restartedPod.Name)
	}

	secret, err := s.waitForStateSecret(ctx, s.restartedPod.Name)
	if err != nil {
		return err
	}
	if secret.UID != expectedUID {
		return fmt.Errorf(
			"tailscale state Secret %s was recreated: got UID %s, expected %s",
			secret.Name,
			secret.UID,
			expectedUID,
		)
	}
	return nil
}

func (s *Tailscale) waitForUnauthenticatedStatus(ctx context.Context, podName string) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Tailscale sidecar in login pod %s waiting for authentication", podName),
		tailscaleStatusTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			output, runErr := s.runtime.Kubectl().Run(waitCtx,
				"exec", "-n", framework.SoperatorNamespace, podName,
				"-c", tailscaleContainerName, "--",
				"tailscale", "status", "--json",
			)
			var status tailscaleStatus
			if err := json.Unmarshal([]byte(output), &status); err != nil {
				if runErr != nil {
					return false, fmt.Errorf("read Tailscale status from login pod %s: %w", podName, runErr)
				}
				return false, fmt.Errorf("decode Tailscale status from login pod %s: %w", podName, err)
			}
			if err := validateUnauthenticatedTailscaleStatus(status); err != nil {
				return false, fmt.Errorf("login pod %s: %w", podName, err)
			}
			return true, nil
		},
	)
}

func (s *Tailscale) waitForStateSecret(ctx context.Context, name string) (corev1.Secret, error) {
	var secret corev1.Secret
	err := s.runtime.WaitFor(ctx, fmt.Sprintf("Tailscale state Secret %s", name),
		tailscaleStatusTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			if err := s.kubectl.GetJSON(waitCtx, &secret,
				"get", "secret", name, "-n", framework.SoperatorNamespace, "-o", "json",
			); err != nil {
				return false, err
			}
			if secret.UID == "" {
				return false, fmt.Errorf("tailscale state Secret %s has no UID", name)
			}
			return true, nil
		},
	)
	if err != nil {
		return corev1.Secret{}, err
	}
	return secret, nil
}

func validateTailscaleSidecar(pod corev1.Pod) error {
	for _, container := range pod.Spec.InitContainers {
		if container.Name != tailscaleContainerName {
			continue
		}
		if container.RestartPolicy == nil || *container.RestartPolicy != corev1.ContainerRestartPolicyAlways {
			return fmt.Errorf("login pod %s Tailscale init container does not use restartPolicy=Always", pod.Name)
		}
		return nil
	}
	return fmt.Errorf("login pod %s has no Tailscale init container", pod.Name)
}

func validateUnauthenticatedTailscaleStatus(status tailscaleStatus) error {
	if status.BackendState != "NeedsLogin" {
		return fmt.Errorf("tailscale BackendState=%q, expected NeedsLogin", status.BackendState)
	}
	if strings.TrimSpace(status.AuthURL) == "" {
		return fmt.Errorf("tailscale AuthURL is empty")
	}
	if !status.TUN {
		return fmt.Errorf("tailscale is not using the kernel TUN device")
	}
	return nil
}
