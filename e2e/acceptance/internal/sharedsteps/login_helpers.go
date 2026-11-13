package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	loginSSHDContainer       = "sshd"
	loginHPAResource         = "horizontalpodautoscalers.autoscaling"
	loginInstanceLabel       = "app.kubernetes.io/instance"
	loginComponentLabel      = "app.kubernetes.io/component"
	loginComponentLabelValue = "login"
)

type loginWorkload struct {
	info    *framework.ClusterInfo
	runtime framework.Runtime
	kubectl *framework.KubectlClient
}

func (s *loginWorkload) loginStatefulSet(ctx context.Context) (kubeobjects.LoginStatefulSet, error) {
	var statefulSets kubeobjects.LoginStatefulSetList
	if err := s.kubectl.GetJSON(ctx, &statefulSets,
		"get", kruiseStatefulSetResource,
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

func (s *loginWorkload) loginPods(ctx context.Context) ([]corev1.Pod, error) {
	var pods corev1.PodList
	if err := s.kubectl.GetJSON(ctx, &pods,
		"get", "pods", "-n", framework.SoperatorNamespace,
		"-l", s.loginSelector(), "-o", "json",
	); err != nil {
		return nil, fmt.Errorf("list login pods: %w", err)
	}
	return pods.Items, nil
}

func (s *loginWorkload) loginSelector() string {
	return fmt.Sprintf("%s=%s,%s=%s",
		loginInstanceLabel, s.info.SlurmClusterName,
		loginComponentLabel, loginComponentLabelValue,
	)
}

func (s *loginWorkload) patchLogin(ctx context.Context, loginPatch map[string]any) error {
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

func (s *loginWorkload) loginHPA(ctx context.Context, name string) (autoscalingv2.HorizontalPodAutoscaler, bool, error) {
	var hpa autoscalingv2.HorizontalPodAutoscaler
	output, err := s.runtime.Kubectl().Run(ctx,
		"get", loginHPAResource, name,
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

func loginFeatureEnabled(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	var feature struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &feature); err != nil {
		return false, fmt.Errorf("decode login feature state: %w", err)
	}
	return feature.Enabled, nil
}

func (s *loginWorkload) waitLoginHPA(ctx context.Context, name string, expected bool, timeout time.Duration) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("login HPA %s existence=%t", name, expected), timeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			_, exists, err := s.loginHPA(waitCtx, name)
			return exists == expected, err
		})
}
