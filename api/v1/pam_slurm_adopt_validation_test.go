package v1_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

func TestPAMSlurmAdoptCRDValidation(t *testing.T) {
	data, err := os.ReadFile("../../config/crd/bases/slurm.nebius.ai_slurmclusters.yaml")
	require.NoError(t, err)

	var crd apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(data, &crd))

	var adoptSchema apiextensions.JSONSchemaProps
	for _, version := range crd.Spec.Versions {
		if version.Name != "v1" {
			continue
		}
		adopt := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["pamSlurmAdopt"]
		require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
			&adopt,
			&adoptSchema,
			nil,
		))
	}

	structural, err := schema.NewStructural(&adoptSchema)
	require.NoError(t, err)
	schemaValidator, _, err := validation.NewSchemaValidator(&adoptSchema)
	require.NoError(t, err)

	tests := []struct {
		name      string
		config    map[string]any
		wantErr   string
		wantValue map[string]any
	}{
		{
			name:      "defaults",
			config:    map[string]any{},
			wantValue: map[string]any{"enabled": false, "actionUnknown": "newest"},
		},
		{
			name: "enabled with exemptions",
			config: map[string]any{
				"enabled":       true,
				"actionUnknown": "deny",
				"exemptUsers":   []any{"service-user"},
				"exemptGroups":  []any{"cluster-admins"},
			},
		},
		{
			name:      "unsupported ambiguity action",
			config:    map[string]any{"actionUnknown": "allow"},
			wantErr:   "actionUnknown",
			wantValue: map[string]any{"enabled": false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaulting.Default(tt.config, structural)
			errs := validation.ValidateCustomResource(
				field.NewPath("spec", "pamSlurmAdopt"),
				tt.config,
				schemaValidator,
			)
			if tt.wantErr != "" {
				require.ErrorContains(t, errs.ToAggregate(), tt.wantErr)
			} else {
				require.Empty(t, errs)
			}
			for key, value := range tt.wantValue {
				require.Equal(t, value, tt.config[key])
			}
		})
	}
}
