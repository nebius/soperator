package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	loginAutoscalingParentRelease = "soperator-fluxcd"
	loginAutoscalingChildRelease  = "flux-system-soperator-fluxcd-slurm-cluster"
)

type loginHelmRelease struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		Suspend bool           `json:"suspend"`
		Values  map[string]any `json:"values"`
	} `json:"spec"`
	Status helmReleaseStatusRef `json:"status"`
}

func (s *LoginAutoscaling) loginHelmRelease(ctx context.Context, name string) (loginHelmRelease, error) {
	var release loginHelmRelease
	err := s.kubectl.GetJSON(ctx, &release, "get", "helmreleases.helm.toolkit.fluxcd.io", name,
		"-n", clusterCreationHelmNamespace, "-o", "json")
	if err != nil {
		return release, fmt.Errorf("get login HelmRelease %s: %w", name, err)
	}
	return release, nil
}

func loginHelmReleaseReady(release loginHelmRelease) error {
	ready := findMetaCondition(release.Status.Conditions, "Ready")
	if release.Spec.Suspend || ready == nil || ready.Status != metav1.ConditionTrue ||
		ready.ObservedGeneration != release.Metadata.Generation {
		return fmt.Errorf("login HelmRelease %s generation=%d suspended=%t Ready=%+v",
			release.Metadata.Name, release.Metadata.Generation, release.Spec.Suspend, ready)
	}
	return nil
}

func (s *LoginAutoscaling) updateLoginHelmValues(ctx context.Context, size int32, enabled, restore bool) error {
	release, err := s.loginHelmRelease(ctx, loginAutoscalingParentRelease)
	if err != nil {
		return err
	}
	if release.Spec.Suspend {
		return fmt.Errorf("login HelmRelease %s is suspended", release.Metadata.Name)
	}
	values := release.Spec.Values
	if values == nil {
		values = make(map[string]any)
	}
	sizePath := []string{"slurmCluster", "overrideValues", "slurmNodes", "login", "size"}
	enabledPath := []string{"slurmCluster", "overrideValues", "slurmNodes", "login", "autoscaling", "enabled"}
	if restore {
		restoreLoginHelmValue(values, s.initialHelmValues, sizePath)
		restoreLoginHelmValue(values, s.initialHelmValues, enabledPath)
	} else {
		setLoginHelmValue(values, sizePath, size)
		setLoginHelmValue(values, enabledPath, enabled)
	}
	// Test resourceVersion before replacing values so concurrent edits are never overwritten.
	patch := []map[string]any{
		{"op": "test", "path": "/metadata/resourceVersion", "value": release.Metadata.ResourceVersion},
		{"op": "add", "path": "/spec/values", "value": values},
	}
	if restore && len(values) == 0 && s.initialHelmValues == nil {
		if release.Spec.Values == nil {
			patch = patch[:1]
		} else {
			patch[1] = map[string]any{"op": "remove", "path": "/spec/values"}
		}
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal login Helm values patch: %w", err)
	}
	if _, err := s.runtime.Kubectl().Run(ctx, "patch", "helmreleases.helm.toolkit.fluxcd.io",
		loginAutoscalingParentRelease, "-n", clusterCreationHelmNamespace, "--type=json", "-p", string(data)); err != nil {
		return fmt.Errorf("patch login Helm values: %w", err)
	}
	for _, name := range []string{loginAutoscalingParentRelease, loginAutoscalingChildRelease} {
		if err := s.runtime.WaitFor(ctx, "ready login HelmRelease "+name,
			loginAutoscalingTimeout, framework.DefaultPollInterval,
			func(waitCtx context.Context) (bool, error) {
				current, err := s.loginHelmRelease(waitCtx, name)
				if err != nil {
					return false, err
				}
				err = loginHelmReleaseReady(current)
				return err == nil, err
			}); err != nil {
			return err
		}
	}
	return s.waitForLoginConfig(ctx, size, enabled)
}

func setLoginHelmValue(values map[string]any, path []string, value any) {
	for _, key := range path[:len(path)-1] {
		next, _ := values[key].(map[string]any)
		if next == nil {
			next = make(map[string]any)
			values[key] = next
		}
		values = next
	}
	values[path[len(path)-1]] = value
}

func restoreLoginHelmValue(current, original map[string]any, path []string) {
	key := path[0]
	value, existed := original[key]
	if len(path) == 1 {
		if existed {
			current[key] = value
		} else {
			delete(current, key)
		}
		return
	}
	child, ok := current[key].(map[string]any)
	if !ok {
		return
	}
	originalChild, _ := value.(map[string]any)
	restoreLoginHelmValue(child, originalChild, path[1:])
	if len(child) == 0 {
		if !existed {
			delete(current, key)
		} else if value == nil {
			current[key] = nil
		}
	}
}

func (s *LoginAutoscaling) waitForLoginConfig(ctx context.Context, size int32, enabled bool) error {
	return s.runtime.WaitFor(ctx, "SlurmCluster login configuration from Helm",
		loginAutoscalingTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			var cluster kubeobjects.LoginAutoscalingCluster
			if err := s.kubectl.GetJSON(waitCtx, &cluster, "get", "slurmcluster", s.info.SlurmClusterName,
				"-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
				return false, err
			}
			err := validateLoginAutoscalingConfig(cluster, size, enabled)
			return err == nil, err
		})
}

func validateLoginAutoscalingConfig(cluster kubeobjects.LoginAutoscalingCluster, size int32, enabled bool) error {
	login := cluster.Spec.SlurmNodes.Login
	var autoscaling struct {
		Enabled   *bool `json:"enabled"`
		Min       int32 `json:"minReplicas"`
		Max       int32 `json:"maxReplicas"`
		TargetCPU int32 `json:"targetCPUUtilizationPercentage"`
	}
	if err := json.Unmarshal(login.Autoscaling, &autoscaling); err != nil {
		return fmt.Errorf("decode login autoscaling configuration: %w", err)
	}
	if login.Size != size || autoscaling.Enabled == nil || *autoscaling.Enabled != enabled ||
		autoscaling.Min != 2 || autoscaling.Max != 3 || autoscaling.TargetCPU != loginAutoscalingTargetCPU {
		return fmt.Errorf("login size=%d autoscaling=%s; expected size=%d enabled=%t min=2 max=3 CPU=%d",
			login.Size, login.Autoscaling, size, enabled, loginAutoscalingTargetCPU)
	}
	return nil
}
