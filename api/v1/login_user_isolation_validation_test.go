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

func TestLoginUserIsolationCRDValidation(t *testing.T) {
	data, err := os.ReadFile("../../config/crd/bases/slurm.nebius.ai_slurmclusters.yaml")
	require.NoError(t, err)

	var crd apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(data, &crd))

	var isolationSchema apiextensions.JSONSchemaProps
	for _, version := range crd.Spec.Versions {
		if version.Name != "v1" {
			continue
		}
		login := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["slurmNodes"].Properties["login"]
		isolation := login.Properties["userIsolation"]
		require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
			&isolation,
			&isolationSchema,
			nil,
		))
	}
	require.Len(t, isolationSchema.XValidations, 2)

	structural, err := schema.NewStructural(&isolationSchema)
	require.NoError(t, err)
	celValidator := cel.NewValidator(structural, false, celconfig.PerCallLimit)
	require.NotNil(t, celValidator)
	schemaValidator, _, err := validation.NewSchemaValidator(&isolationSchema)
	require.NoError(t, err)

	tests := []struct {
		name      string
		isolation map[string]any
		wantErr   string
	}{
		{
			name:      "both limits omitted",
			isolation: map[string]any{"enabled": true},
		},
		{
			name: "both limits specified",
			isolation: map[string]any{
				"enabled": true, "memoryHigh": "4Gi", "memoryMax": "5Gi",
			},
		},
		{
			name: "only memoryHigh specified",
			isolation: map[string]any{
				"enabled": true, "memoryHigh": "4Gi",
			},
			wantErr: "memoryHigh and memoryMax must be specified together or both omitted",
		},
		{
			name: "only memoryMax specified",
			isolation: map[string]any{
				"enabled": true, "memoryMax": "5Gi",
			},
			wantErr: "memoryHigh and memoryMax must be specified together or both omitted",
		},
		{
			name: "memoryHigh exceeds memoryMax",
			isolation: map[string]any{
				"enabled": true, "memoryHigh": "6Gi", "memoryMax": "5Gi",
			},
			wantErr: "memoryHigh must not exceed memoryMax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaulting.Default(tt.isolation, structural)
			path := field.NewPath("spec", "slurmNodes", "login", "userIsolation")
			errs := validation.ValidateCustomResource(path, tt.isolation, schemaValidator)
			celErrs, _ := celValidator.Validate(
				context.Background(),
				path,
				structural,
				tt.isolation,
				nil,
				celconfig.RuntimeCELCostBudget,
			)
			errs = append(errs, celErrs...)
			if tt.wantErr != "" {
				require.ErrorContains(t, errs.ToAggregate(), tt.wantErr)
			} else {
				require.Empty(t, errs)
			}
		})
	}
}
