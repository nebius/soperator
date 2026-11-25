package sharedsteps

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func TestValidateProvisionedLoginAutoscaling(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  string
		size    int32
		wantErr bool
	}{
		{"provisioned", `{"enabled":true,"minReplicas":2,"maxReplicas":3,"targetCPUUtilizationPercentage":10}`, 2, false},
		{"missing", `null`, 2, true},
		{"disabled", `{"enabled":false,"minReplicas":2,"maxReplicas":3,"targetCPUUtilizationPercentage":10}`, 2, true},
		{"wrong minimum", `{"enabled":true,"minReplicas":1,"maxReplicas":3,"targetCPUUtilizationPercentage":10}`, 2, true},
		{"wrong maximum", `{"enabled":true,"minReplicas":2,"maxReplicas":10,"targetCPUUtilizationPercentage":10}`, 2, true},
		{"product target instead of fixture", `{"enabled":true,"minReplicas":2,"maxReplicas":3,"targetCPUUtilizationPercentage":70}`, 2, true},
		{"wrong fixed size", `{"enabled":true,"minReplicas":2,"maxReplicas":3,"targetCPUUtilizationPercentage":10}`, 1, true},
		{"malformed", `{`, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cluster kubeobjects.LoginAutoscalingCluster
			cluster.Spec.SlurmNodes.Login.Size = tc.size
			cluster.Spec.SlurmNodes.Login.Autoscaling = jsontext.Value(tc.config)
			err := validateLoginAutoscalingConfig(cluster, 2, true)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoginHelmReleaseReadyRequiresCurrentGeneration(t *testing.T) {
	var release loginHelmRelease
	release.Metadata = metav1.ObjectMeta{Name: "test", Generation: 4}
	require.Error(t, loginHelmReleaseReady(release))
	release.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 3}}
	require.ErrorContains(t, loginHelmReleaseReady(release), "generation=4")
	release.Status.Conditions[0].ObservedGeneration = 4
	require.NoError(t, loginHelmReleaseReady(release))
	release.Spec.Suspend = true
	require.ErrorContains(t, loginHelmReleaseReady(release), "suspended=true")
}

func TestRestoreLoginHelmValues(t *testing.T) {
	for _, originalJSON := range []string{
		`{}`,
		`{"slurmCluster":null}`,
		`{"slurmCluster":{"overrideValues":{"slurmNodes":{"login":{"size":null,"autoscaling":{"enabled":null,"maxReplicas":3}}}}}}`,
		`{"slurmCluster":{"overrideValues":{"slurmNodes":{"login":{"size":2,"autoscaling":{"enabled":true,"maxReplicas":3}}}}}}`,
	} {
		t.Run(originalJSON, func(t *testing.T) {
			var original, current map[string]any
			require.NoError(t, json.Unmarshal([]byte(originalJSON), &original))
			require.NoError(t, json.Unmarshal([]byte(originalJSON), &current))
			path := []string{"slurmCluster", "overrideValues", "slurmNodes", "login"}
			sizePath := append(slices.Clone(path), "size")
			enabledPath := append(slices.Clone(path), "autoscaling", "enabled")
			setLoginHelmValue(current, sizePath, 1)
			setLoginHelmValue(current, enabledPath, false)
			current["unrelated"] = "concurrent change"
			restoreLoginHelmValue(current, original, sizePath)
			restoreLoginHelmValue(current, original, enabledPath)
			assert.Equal(t, "concurrent change", current["unrelated"])
			delete(current, "unrelated")
			assert.Equal(t, original, current)
		})
	}
}

// The fake models Flux applying parent overrides onto Terraform's provisioned baseline.
// It deliberately exposes only Kubernetes operations; any CR mutation fails the test.
type loginAutoscalingRuntime struct {
	framework.Runtime
	t           *testing.T
	values      map[string]any
	generation  int64
	patches     int
	failPatch   int
	staleReads  int
	hpaOverride string
	logs        []string
}

func (r *loginAutoscalingRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		encode := func(value any) string {
			data, err := json.Marshal(value)
			require.NoError(r.t, err)
			return string(data)
		}
		switch {
		case args[0] == "patch":
			require.Equal(r.t, "helmreleases.helm.toolkit.fluxcd.io", args[1])
			require.Equal(r.t, loginAutoscalingParentRelease, args[2])
			require.Contains(r.t, args, "--type=json")
			var patch []struct {
				Op, Path string
				Value    any
			}
			require.NoError(r.t, json.Unmarshal([]byte(args[len(args)-1]), &patch))
			require.Equal(r.t, "test", patch[0].Op)
			require.Equal(r.t, "/metadata/resourceVersion", patch[0].Path)
			require.Equal(r.t, fmt.Sprint(r.generation), patch[0].Value)
			if len(patch) > 1 {
				require.Equal(r.t, "/spec/values", patch[1].Path)
				r.values, _ = patch[1].Value.(map[string]any)
			}
			r.generation++
			r.patches++
			if r.patches == r.failPatch {
				return "", errors.New("response lost after patch")
			}
			return "", nil
		case args[0] == "get" && args[1] == "helmreleases.helm.toolkit.fluxcd.io":
			var release loginHelmRelease
			release.Metadata = metav1.ObjectMeta{Name: args[2], Generation: r.generation, ResourceVersion: fmt.Sprint(r.generation)}
			release.Spec.Values = r.values
			observed := r.generation
			if r.staleReads > 0 {
				observed--
				r.staleReads--
			}
			release.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: observed}}
			return encode(release), nil
		case args[0] == "get" && args[1] == "slurmcluster":
			size, enabled := r.effectiveLogin()
			return fmt.Sprintf(`{"spec":{"slurmNodes":{"login":{"size":%d,"autoscaling":{"enabled":%t,"minReplicas":2,"maxReplicas":3,"targetCPUUtilizationPercentage":10}}}}}`, size, enabled), nil
		case args[0] == "get" && args[1] == loginAutoscalingHPAResource:
			_, enabled := r.effectiveLogin()
			if !enabled {
				return "", nil
			}
			if r.hpaOverride != "" {
				return r.hpaOverride, nil
			}
			return `{"spec":{"minReplicas":2,"maxReplicas":3,"metrics":[{"type":"ContainerResource","containerResource":{"container":"sshd","name":"cpu","target":{"type":"Utilization","averageUtilization":10}}}]}}`, nil
		case args[0] == "get" && args[1] == kruiseStatefulSetResource:
			size, _ := r.effectiveLogin()
			return fmt.Sprintf(`{"items":[{"metadata":{"name":"login"},"spec":{"replicas":%d},"status":{"readyReplicas":%d}}]}`, size, size), nil
		case args[0] == "get" && args[1] == "pods":
			size, _ := r.effectiveLogin()
			var items []any
			for i := range size {
				items = append(items, map[string]any{
					"metadata": map[string]any{"name": fmt.Sprintf("login-%d", i), "uid": fmt.Sprint(i)},
					"status":   map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
				})
			}
			return encode(map[string]any{"items": items}), nil
		case args[0] == "exec":
			require.Contains(r.t, args[len(args)-1], "kill")
			return "", nil
		default:
			r.t.Fatalf("unexpected Kubernetes operation: %v", args)
			return "", nil
		}
	})
}

func (r *loginAutoscalingRuntime) effectiveLogin() (int, bool) {
	size, enabled := 2, true
	values := r.values
	for _, key := range []string{"slurmCluster", "overrideValues", "slurmNodes", "login"} {
		values, _ = values[key].(map[string]any)
	}
	if n, ok := values["size"].(float64); ok {
		size = int(n)
	}
	autoscaling, _ := values["autoscaling"].(map[string]any)
	if value, ok := autoscaling["enabled"].(bool); ok {
		enabled = value
	}
	return size, enabled
}

func (r *loginAutoscalingRuntime) WaitFor(ctx context.Context, description string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	var err error
	for range 3 {
		var ready bool
		ready, err = condition(ctx)
		if ready {
			return nil
		}
	}
	return fmt.Errorf("%s: %w", description, err)
}

func (r *loginAutoscalingRuntime) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

func TestLoginAutoscalingHelmLifecycleAndCleanup(t *testing.T) {
	for _, failPatch := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(failPatch), func(t *testing.T) {
			runtime := &loginAutoscalingRuntime{t: t, generation: 1, failPatch: failPatch}
			steps := NewLoginAutoscaling(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
			require.NoError(t, steps.selectReadyLoginWorkload(t.Context()))
			assert.Zero(t, runtime.patches, "setup must only inspect provisioned configuration")
			runtime.staleReads = 2
			err := steps.changeFixedLoginSize(t.Context())
			if failPatch == 1 {
				require.ErrorContains(t, err, "response lost")
			} else {
				require.NoError(t, err)
				require.NoError(t, steps.disableLoginAutoscaling(t.Context()))
				require.NoError(t, steps.waitForLoginScaleDown(t.Context()))
			}
			steps.pressurePods = []string{"login-0"}
			steps.CleanupAndReset(t.Context())
			assert.Nil(t, runtime.values, "restore absence of original inline Helm values")
			assert.Empty(t, steps.pressurePods)
			assert.False(t, steps.initialConfigCaptured)
			if failPatch == 3 {
				assert.Contains(t, strings.Join(runtime.logs, "\n"), "response lost")
			} else {
				assert.Empty(t, runtime.logs)
			}
			patches := runtime.patches
			steps.CleanupAndReset(t.Context())
			assert.Equal(t, patches, runtime.patches, "cleanup must be idempotent")
		})
	}
}

func TestLoginAutoscalingRejectsIncorrectHPAWithoutMutation(t *testing.T) {
	for _, hpa := range []string{
		`{"spec":{"minReplicas":1,"maxReplicas":3}}`,
		`{"spec":{"minReplicas":2,"maxReplicas":3,"metrics":[{"type":"ContainerResource","containerResource":{"container":"munge","name":"cpu","target":{"type":"Utilization","averageUtilization":10}}}]}}`,
		`{"spec":{"minReplicas":2,"maxReplicas":3,"metrics":[{"type":"ContainerResource","containerResource":{"container":"sshd","name":"cpu","target":{"type":"Utilization","averageUtilization":70}}}]}}`,
	} {
		runtime := &loginAutoscalingRuntime{t: t, generation: 1, hpaOverride: hpa}
		steps := NewLoginAutoscaling(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
		require.Error(t, steps.selectReadyLoginWorkload(t.Context()))
		steps.CleanupAndReset(t.Context())
		assert.Zero(t, runtime.patches)
	}
}
