package common

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"sigs.k8s.io/yaml"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderSlurmConfigMapTopologyParam(t *testing.T) {
	const defaultTopologyParam = "BlockAsNodeRank,SwitchAsNodeRank"

	data, err := os.ReadFile("../../../helm/slurm-cluster/values.yaml")
	require.NoError(t, err)
	var chart struct {
		SlurmConfig map[string]any `json:"slurmConfig"`
	}
	require.NoError(t, yaml.Unmarshal(data, &chart))
	require.Equal(t, defaultTopologyParam, chart.SlurmConfig["topologyParam"])

	data, err = os.ReadFile("../../../config/crd/bases/slurm.nebius.ai_slurmclusters.yaml")
	require.NoError(t, err)
	var crd apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(data, &crd))
	var slurmConfigSchema apiextensions.JSONSchemaProps
	for _, version := range crd.Spec.Versions {
		if version.Name == "v1" {
			config := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["slurmConfig"]
			require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
				&config, &slurmConfigSchema, nil,
			))
		}
	}
	require.Contains(t, slurmConfigSchema.Properties, "topologyParam")
	structural, err := schema.NewStructural(&slurmConfigSchema)
	require.NoError(t, err)

	for _, tt := range []struct {
		name   string
		config map[string]any
		want   string
	}{
		{
			name:   "Helm default",
			config: chart.SlurmConfig,
			want:   defaultTopologyParam,
		},
		{
			name:   "CRD default",
			config: map[string]any{},
			want:   defaultTopologyParam,
		},
		{
			name:   "explicit block ranking",
			config: map[string]any{"topologyParam": "BlockAsNodeRank"},
			want:   "BlockAsNodeRank",
		},
		{
			name:   "explicit switch ranking",
			config: map[string]any{"topologyParam": "SwitchAsNodeRank"},
			want:   "SwitchAsNodeRank",
		},
		{
			name:   "explicit other options",
			config: map[string]any{"topologyParam": "RouteTree"},
			want:   "RouteTree",
		},
		{
			name:   "explicit empty value",
			config: map[string]any{"topologyParam": ""},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defaulting.Default(tt.config, structural)
			data, err := json.Marshal(tt.config)
			require.NoError(t, err)
			var config slurmv1.SlurmConfig
			require.NoError(t, json.Unmarshal(data, &config))
			require.Equal(t, tt.want, config.TopologyParam)

			result := RenderConfigMapSlurmConfigs(&values.SlurmCluster{SlurmConfig: config})
			require.Contains(t, strings.Split(result.Data[consts.ConfigMapKeySlurmConfig], "\n"),
				"include /etc/slurm/slurm_base.conf.noedit")
			var topologyParams []string
			for _, line := range strings.Split(result.Data[consts.ConfigMapKeySlurmBaseConfig], "\n") {
				if strings.HasPrefix(line, "TopologyParam=") {
					topologyParams = append(topologyParams, line)
				}
			}
			if tt.want == "" {
				require.Empty(t, topologyParams)
			} else {
				require.Equal(t, []string{"TopologyParam=" + tt.want}, topologyParams)
			}
		})
	}
}
