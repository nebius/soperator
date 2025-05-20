package prometheus_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	. "nebius.ai/slurm-operator/internal/render/prometheus"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderDeploymentExporter_Error(t *testing.T) {

	exporterNil := &values.SlurmExporter{}

	exporterImageNil := &values.SlurmExporter{
		Enabled: true,
	}

	testCases := []struct {
		valuesExporter *values.SlurmExporter
		expectedError  error
	}{
		{
			valuesExporter: exporterNil,
			expectedError:  errors.New("prometheus is not enabled"),
		},
		{
			valuesExporter: exporterImageNil,
			expectedError:  errors.New("image for ContainerExporter is empty"),
		},
	}

	for _, tc := range testCases {
		t.Run("exporter", func(t *testing.T) {

			_, err := RenderDeploymentExporter(
				defaultNamespace, defaultNameCluster, tc.valuesExporter, defaultNodeFilter, defaultVolumeSources,
				defaultPodTemplate, defaultSlurmAPIServer,
			)
			if err == nil {
				t.Errorf("expected error, got nil")
			}

			assert.Equal(t, tc.expectedError, err)
		})
	}
}
