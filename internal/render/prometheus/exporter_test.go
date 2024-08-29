package prometheus_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	. "nebius.ai/slurm-operator/internal/render/prometheus"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderDeploymentExporter_Error(t *testing.T) {

	telemetryNil := &values.SlurmExporter{}

	telemetryImageSlurmExporterNil := &values.SlurmExporter{
		MetricsPrometheus: slurmv1.MetricsPrometheus{
			Enabled:            true,
			ImageSlurmExporter: nil,
		},
		Name: "test",
	}

	testCases := []struct {
		valuesExporter *values.SlurmExporter
		expectedError  error
	}{
		{
			valuesExporter: telemetryNil,
			expectedError:  errors.New("prometheus is not enabled"),
		},
		{
			valuesExporter: telemetryImageSlurmExporterNil,
			expectedError:  errors.New("ImageSlurmExporter is nil"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.valuesExporter.Name, func(t *testing.T) {

			_, err := RenderDeploymentExporter(
				defaultNamespace, defaultNameCluster, tc.valuesExporter, defaultNodeFilter, defaultVolumeSources, defaultPodTemplate,
			)
			if err == nil {
				t.Errorf("expected error, got nil")
			}

			assert.Equal(t, tc.expectedError, err)
		})
	}
}
