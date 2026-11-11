package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestHealthCheckFailureChecksCapabilityBeforeInjection(t *testing.T) {
	deployment := func(args ...string) appsv1.Deployment {
		return appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "soperatorchecks-checks", Namespace: framework.SoperatorSystemNamespace},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "manager", Args: args}},
			}}},
		}
	}
	missingManager := deployment("--enable-node-replacement=true")
	missingManager.Spec.Template.Spec.Containers[0].Name = "sidecar"
	for _, tt := range []struct {
		name        string
		deployments []appsv1.Deployment
		output      string
		queryErr    error
		wantSkip    bool
		wantError   string
		injections  int
	}{
		{name: "disabled skips", deployments: []appsv1.Deployment{deployment("--enable-node-replacement=false")}, wantSkip: true},
		{name: "enabled injects fault", deployments: []appsv1.Deployment{deployment("--enable-node-replacement=true")}, injections: 1},
		{name: "missing deployment skips", wantSkip: true},
		{name: "ambiguous deployments fail", deployments: []appsv1.Deployment{deployment(), deployment()}, wantError: "found 2"},
		{name: "missing manager fails", deployments: []appsv1.Deployment{missingManager}, wantError: "manager container is missing"},
		{name: "malformed response fails", output: "{", wantError: "decode kubectl"},
		{name: "query failure fails", queryErr: errors.New("forbidden"), wantError: "read health-check replacement configuration: forbidden"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				output := tt.output
				if output == "" {
					data, err := json.Marshal(appsv1.DeploymentList{Items: tt.deployments})
					require.NoError(t, err)
					output = string(data)
				}
				var injections int
				runtime := healthCheckReplacementRuntime{
					kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
						require.Equal(t, []string{"get", "deployments", "-n", "soperator-system", "-l", "control-plane=soperatorchecks", "-o", "json"}, args)
						return output, tt.queryErr
					}),
					worker: framework.NewCommandScope(func(context.Context, string) (string, error) {
						injections++
						return "", nil
					}),
				}
				steps := NewNodeReplacement(runtime, nil, nil)
				steps.replacementWorker = framework.WorkerInfo{Name: "worker-1"}

				err := steps.aGPUHealthCheckFailureIsSimulatedOnThatNode(t.Context())

				switch {
				case tt.wantSkip:
					require.ErrorIs(t, err, godog.ErrSkip)
				case tt.wantError != "":
					require.ErrorContains(t, err, tt.wantError)
					assert.NotErrorIs(t, err, godog.ErrSkip)
				default:
					require.NoError(t, err)
				}
				assert.Equal(t, tt.injections, injections)
			})
		})
	}
}
