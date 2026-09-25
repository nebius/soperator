package v1_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"sigs.k8s.io/yaml"
)

func TestLoginAutoscalingCRDValidation(t *testing.T) {
	data, err := os.ReadFile("../../config/crd/bases/slurm.nebius.ai_slurmclusters.yaml")
	require.NoError(t, err)
	var crd apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(data, &crd))
	var loginSchema apiextensions.JSONSchemaProps
	for _, version := range crd.Spec.Versions {
		if version.Name == "v1" {
			login := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["slurmNodes"].Properties["login"]
			require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&login, &loginSchema, nil))
		}
	}
	require.NotEmpty(t, loginSchema.XValidations)
	require.NotEmpty(t, loginSchema.Properties["autoscaling"].XValidations)
	structural, err := schema.NewStructural(&loginSchema)
	require.NoError(t, err)
	celValidator := cel.NewValidator(structural, false, celconfig.PerCallLimit)
	require.NotNil(t, celValidator)
	schemaValidator, _, err := validation.NewSchemaValidator(&loginSchema)
	require.NoError(t, err)

	tests := []struct {
		name    string
		change  func(login, autoscaling, sshd map[string]any)
		wantErr string
	}{
		{name: "valid"},
		{name: "equal replica bounds", change: func(_, autoscaling, _ map[string]any) {
			autoscaling["maxReplicas"] = int64(2)
		}},
		{name: "login size does not constrain bounds", change: func(login, _, _ map[string]any) {
			login["size"] = int64(10)
		}},
		{name: "autoscaling omitted", change: func(login, _, sshd map[string]any) {
			delete(login, "autoscaling")
			delete(sshd, "resources")
		}},
		{name: "disabled ignores CPU and replica ordering", change: func(_, autoscaling, sshd map[string]any) {
			autoscaling["enabled"] = false
			autoscaling["maxReplicas"] = int64(1)
			delete(sshd, "resources")
		}},
		{name: "max replicas below minimum", change: func(_, autoscaling, _ map[string]any) {
			autoscaling["maxReplicas"] = int64(1)
		}, wantErr: "maxReplicas must be at least minReplicas"},
		{name: "zero minimum", change: func(_, autoscaling, _ map[string]any) {
			autoscaling["minReplicas"] = int64(0)
		}, wantErr: "minReplicas"},
		{name: "zero maximum", change: func(_, autoscaling, _ map[string]any) {
			autoscaling["maxReplicas"] = int64(0)
		}, wantErr: "maxReplicas"},
		{name: "missing enabled", change: func(_, autoscaling, _ map[string]any) {
			delete(autoscaling, "enabled")
		}, wantErr: "enabled"},
		{name: "missing minimum", change: func(_, autoscaling, _ map[string]any) {
			delete(autoscaling, "minReplicas")
		}, wantErr: "minReplicas"},
		{name: "missing maximum", change: func(_, autoscaling, _ map[string]any) {
			delete(autoscaling, "maxReplicas")
		}, wantErr: "maxReplicas"},
		{name: "missing resources", change: func(_, _, sshd map[string]any) {
			delete(sshd, "resources")
		}, wantErr: "sshd.resources.cpu must be greater than zero"},
		{name: "missing CPU", change: func(_, _, sshd map[string]any) {
			sshd["resources"] = map[string]any{"memory": "1Gi"}
		}, wantErr: "sshd.resources.cpu must be greater than zero"},
		{name: "default target", change: func(_, autoscaling, _ map[string]any) {
			delete(autoscaling, "targetCPUUtilizationPercentage")
		}},
		{name: "minimum target", change: func(_, autoscaling, _ map[string]any) {
			autoscaling["targetCPUUtilizationPercentage"] = int64(1)
		}},
		{name: "maximum target", change: func(_, autoscaling, _ map[string]any) {
			autoscaling["targetCPUUtilizationPercentage"] = int64(100)
		}},
		{name: "target below range", change: func(_, autoscaling, _ map[string]any) {
			autoscaling["targetCPUUtilizationPercentage"] = int64(0)
		}, wantErr: "targetCPUUtilizationPercentage"},
		{name: "target above range", change: func(_, autoscaling, _ map[string]any) {
			autoscaling["targetCPUUtilizationPercentage"] = int64(101)
		}, wantErr: "targetCPUUtilizationPercentage"},
	}
	for _, cpu := range []struct {
		name    string
		value   any
		wantErr string
	}{
		{name: "millicores", value: "100m"},
		{name: "fractional CPU", value: "0.5"},
		{name: "integer CPU", value: int64(1)},
		{name: "zero CPU", value: "0", wantErr: "sshd.resources.cpu must be greater than zero"},
		{name: "zero integer CPU", value: int64(0), wantErr: "sshd.resources.cpu must be greater than zero"},
		{name: "negative CPU", value: "-100m", wantErr: "sshd.resources.cpu must be greater than zero"},
	} {
		tests = append(tests, struct {
			name    string
			change  func(login, autoscaling, sshd map[string]any)
			wantErr string
		}{name: cpu.name, change: func(_, _, sshd map[string]any) {
			sshd["resources"] = map[string]any{"cpu": cpu.value}
		}, wantErr: cpu.wantErr})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			autoscaling := map[string]any{
				"enabled": true, "minReplicas": int64(2), "maxReplicas": int64(4),
				"targetCPUUtilizationPercentage": int64(70),
			}
			sshd := map[string]any{"image": "sshd:test", "resources": map[string]any{"cpu": "1"}}
			login := map[string]any{
				"size": int64(2), "k8sNodeFilterName": "login", "sshd": sshd,
				"munge": map[string]any{"image": "munge:test"}, "sshdServiceType": "LoadBalancer",
				"sshRootPublicKeys": []any{}, "volumes": map[string]any{"jail": map[string]any{}},
				"autoscaling": autoscaling,
			}
			if tt.change != nil {
				tt.change(login, autoscaling, sshd)
			}
			defaulting.Default(login, structural)
			path := field.NewPath("spec", "slurmNodes", "login")
			errs := validation.ValidateCustomResource(path, login, schemaValidator)
			celErrs, _ := celValidator.Validate(context.Background(), path, structural, login, nil, celconfig.RuntimeCELCostBudget)
			errs = append(errs, celErrs...)
			if tt.wantErr != "" {
				require.ErrorContains(t, errs.ToAggregate(), tt.wantErr)
			} else {
				require.Empty(t, errs)
			}
			if tt.name == "default target" {
				require.EqualValues(t, 70, autoscaling["targetCPUUtilizationPercentage"])
			}
		})
	}
}
