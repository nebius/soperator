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

func TestHealthCheckNodeReplacementEnabled(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		want      bool
		wantError bool
	}{
		{name: "binary default", want: true},
		{name: "enabled", args: []string{"--enable-node-replacement=true"}, want: true},
		{name: "disabled", args: []string{"--enable-node-replacement=false"}},
		{name: "bare boolean", args: []string{"--enable-node-replacement"}, want: true},
		{name: "single dash", args: []string{"-enable-node-replacement=false"}},
		{name: "last flag disables", args: []string{"--enable-node-replacement=true", "--enable-node-replacement=false"}},
		{name: "last flag enables", args: []string{"--enable-node-replacement=false", "--enable-node-replacement=true"}, want: true},
		{name: "unrelated flag", args: []string{"--delete-not-ready-nodes=false"}, want: true},
		{name: "invalid value", args: []string{"--enable-node-replacement=invalid"}, wantError: true},
		{name: "empty value", args: []string{"--enable-node-replacement="}, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			enabled, err := healthCheckNodeReplacementEnabled(tt.args)
			if tt.wantError {
				require.ErrorContains(t, err, "parse --enable-node-replacement")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, enabled)
		})
	}
}

func TestHealthCheckReplacementCapabilityPrerequisite(t *testing.T) {
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
	}{
		{name: "disabled skips", deployments: []appsv1.Deployment{deployment("--enable-node-replacement=false")}, wantSkip: true},
		{name: "enabled continues", deployments: []appsv1.Deployment{deployment("--enable-node-replacement=true")}},
		{name: "omitted flag uses binary default", deployments: []appsv1.Deployment{deployment()}},
		{name: "missing deployment fails", wantError: "found 0"},
		{name: "ambiguous deployments fail", deployments: []appsv1.Deployment{deployment(), deployment()}, wantError: "found 2"},
		{name: "missing manager fails", deployments: []appsv1.Deployment{missingManager}, wantError: "manager container is missing"},
		{name: "invalid setting fails", deployments: []appsv1.Deployment{deployment("--enable-node-replacement=invalid")}, wantError: "parse --enable-node-replacement"},
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
				runtime := healthCheckReplacementRuntime{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
					require.Equal(t, []string{"get", "deployments", "-n", "soperator-system", "-l", "control-plane=soperatorchecks", "-o", "json"}, args)
					return output, tt.queryErr
				})}
				steps := NewNodeReplacement(runtime, nil, nil)

				err := steps.healthCheckDrivenNodeReplacementIsEnabled(t.Context())

				switch {
				case tt.wantSkip:
					require.ErrorIs(t, err, godog.ErrSkip)
				case tt.wantError != "":
					require.ErrorContains(t, err, tt.wantError)
					assert.NotErrorIs(t, err, godog.ErrSkip)
				default:
					require.NoError(t, err)
				}
				assert.False(t, steps.faultInjected)
				assert.True(t, steps.maintenanceJob.IsZero())
			})
		})
	}
}
